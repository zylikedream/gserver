package gxyactor

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

const locateBenchLookupScript = `
local rankKey = KEYS[1]
local onlinePrefix = ARGV[1]
local infoPrefix = ARGV[2]
local roleID = ARGV[3]
for _, nodeID in ipairs(redis.call("ZRANGE", rankKey, 0, -1)) do
    if redis.call("SISMEMBER", onlinePrefix .. nodeID, roleID) == 1 then
        local serverInfo = redis.call("GET", infoPrefix .. nodeID)
        if serverInfo then
            return serverInfo
        end
    end
end
return false
`

func BenchmarkActorLocateLookup(b *testing.B) {
	if os.Getenv("RUN_ACTOR_LOCATE_BENCH") != "1" {
		b.Skip("set RUN_ACTOR_LOCATE_BENCH=1 to run the Redis actor locate benchmark")
	}

	ctx := context.Background()
	client := newActorLocateBenchRedis(b)
	config := actorLocateBenchConfigFromEnv(b)
	prefix := fmt.Sprintf("actor-locate-bench:%d", time.Now().UnixNano())
	b.Logf("servers=%d players_per_server=%d total_players=%d redis=%s db=%d prefix=%s", config.servers, config.playersPerServer, config.totalPlayers(), config.addr, config.db, prefix)
	b.Cleanup(func() {
		_ = client.FlushDB(ctx).Err()
		_ = client.Close()
	})

	oldData := newLegacyLocateBenchData(prefix)
	oldStart := time.Now()
	populateLegacyLocateBenchData(b, ctx, client, config, oldData)
	b.Logf("ktm_server dataset setup=%s", time.Since(oldStart))
	logActorLocateBenchMemory(b, ctx, client, "ktm_server")

	legacyScenarios := []struct {
		name   string
		roleID string
	}{
		{name: "first_hit", roleID: locateBenchRoleID(0, 0)},
		{name: "middle_hit", roleID: locateBenchRoleID(config.servers/2, 0)},
		{name: "last_hit", roleID: locateBenchRoleID(config.servers-1, 0)},
		{name: "miss", roleID: "role-missing"},
	}
	for _, scenario := range legacyScenarios {
		scenario := scenario
		b.Run("ktm_server/"+scenario.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, err := client.Eval(ctx, locateBenchLookupScript, []string{oldData.rankKey}, oldData.onlinePrefix, oldData.infoPrefix, scenario.roleID).Result()
				if err != nil && err != redis.Nil {
					b.Fatalf("legacy lookup error = %v", err)
				}
			}
		})
	}

	if err := client.FlushDB(ctx).Err(); err != nil {
		b.Fatalf("flush legacy dataset error = %v", err)
	}

	currentData := newCurrentLocateBenchData(prefix)
	currentStart := time.Now()
	populateCurrentLocateBenchData(b, ctx, client, config, currentData)
	b.Logf("gserver dataset setup=%s", time.Since(currentStart))
	logActorLocateBenchMemory(b, ctx, client, "gserver")

	currentScenarios := []struct {
		name   string
		roleID string
	}{
		{name: "first_hit", roleID: locateBenchRoleID(0, 0)},
		{name: "middle_hit", roleID: locateBenchRoleID(config.servers/2, 0)},
		{name: "last_hit", roleID: locateBenchRoleID(config.servers-1, 0)},
		{name: "miss", roleID: "role-missing"},
	}
	for _, scenario := range currentScenarios {
		scenario := scenario
		b.Run("gserver/"+scenario.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := client.Eval(ctx, actorLocatorLocateScript, []string{currentData.keyPrefix + scenario.roleID}).Result(); err != nil {
					b.Fatalf("current lookup error = %v", err)
				}
			}
		})
	}
}

type actorLocateBenchConfig struct {
	addr             string
	db               int
	servers          int
	playersPerServer int
	poolSize         int
}

func (c actorLocateBenchConfig) totalPlayers() int {
	return c.servers * c.playersPerServer
}

func actorLocateBenchConfigFromEnv(tb testing.TB) actorLocateBenchConfig {
	tb.Helper()
	return actorLocateBenchConfig{
		addr:             actorLocateBenchStringEnv("ACTOR_LOCATE_REDIS_ADDR", "127.0.0.1:6389"),
		db:               actorLocateBenchIntEnv(tb, "ACTOR_LOCATE_REDIS_DB", 15),
		servers:          actorLocateBenchIntEnv(tb, "ACTOR_LOCATE_BENCH_SERVERS", 10),
		playersPerServer: actorLocateBenchIntEnv(tb, "ACTOR_LOCATE_BENCH_PLAYERS_PER_SERVER", 1000),
		poolSize:         actorLocateBenchIntEnv(tb, "ACTOR_LOCATE_REDIS_POOL_SIZE", 128),
	}
}

func actorLocateBenchStringEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func actorLocateBenchIntEnv(tb testing.TB, name string, fallback int) int {
	tb.Helper()
	value := actorLocateBenchStringEnv(name, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		tb.Fatalf("%s must be a positive integer, got %q", name, value)
	}
	return parsed
}

func newActorLocateBenchRedis(tb testing.TB) *redis.Client {
	tb.Helper()
	config := actorLocateBenchConfigFromEnv(tb)
	client := redis.NewClient(&redis.Options{Addr: config.addr, DB: config.db, PoolSize: config.poolSize})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		tb.Fatalf("redis ping error = %v", err)
	}
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		_ = client.Close()
		tb.Fatalf("flush benchmark Redis DB error = %v", err)
	}
	return client
}

type legacyLocateBenchData struct {
	rankKey      string
	onlinePrefix string
	infoPrefix   string
}

func newLegacyLocateBenchData(prefix string) legacyLocateBenchData {
	return legacyLocateBenchData{
		rankKey:      prefix + ":legacy:server-rank",
		onlinePrefix: prefix + ":legacy:online:",
		infoPrefix:   prefix + ":legacy:info:",
	}
}

type currentLocateBenchData struct {
	keyPrefix string
}

func newCurrentLocateBenchData(prefix string) currentLocateBenchData {
	return currentLocateBenchData{keyPrefix: prefix + ":current:locate:"}
}

func populateLegacyLocateBenchData(b testing.TB, ctx context.Context, client *redis.Client, config actorLocateBenchConfig, data legacyLocateBenchData) {
	b.Helper()
	pipeline := client.Pipeline()
	pending := 0
	flush := func() {
		if pending == 0 {
			return
		}
		if _, err := pipeline.Exec(ctx); err != nil {
			b.Fatalf("populate legacy dataset error = %v", err)
		}
		pipeline = client.Pipeline()
		pending = 0
	}

	for server := range config.servers {
		nodeID := locateBenchNodeID(server)
		onlineKey := data.onlinePrefix + nodeID
		pipeline.ZAdd(ctx, data.rankKey, redis.Z{Score: float64(server), Member: nodeID})
		pipeline.Set(ctx, data.infoPrefix+nodeID, nodeID, 0)
		pending += 2

		for offset := 0; offset < config.playersPerServer; offset += 1000 {
			end := offset + 1000
			if end > config.playersPerServer {
				end = config.playersPerServer
			}
			members := make([]interface{}, 0, end-offset)
			for player := offset; player < end; player++ {
				members = append(members, locateBenchRoleID(server, player))
			}
			pipeline.SAdd(ctx, onlineKey, members...)
			pending++
			if pending >= 100 {
				flush()
			}
		}
	}
	flush()
}

func populateCurrentLocateBenchData(b testing.TB, ctx context.Context, client *redis.Client, config actorLocateBenchConfig, data currentLocateBenchData) {
	b.Helper()
	pipeline := client.Pipeline()
	pending := 0
	flush := func() {
		if pending == 0 {
			return
		}
		if _, err := pipeline.Exec(ctx); err != nil {
			b.Fatalf("populate current dataset error = %v", err)
		}
		pipeline = client.Pipeline()
		pending = 0
	}

	for server := range config.servers {
		nodeID := locateBenchNodeID(server)
		token := nodeID + "-lease-token"
		pipeline.Set(ctx, actorLocatorLeaseKey(nodeID), token, 0)
		pending++
		for player := range config.playersPerServer {
			roleID := locateBenchRoleID(server, player)
			epoch := uint64(server*config.playersPerServer + player + 1)
			pipeline.Set(ctx, data.keyPrefix+roleID, encodeActorOwner(ActorOwner{NodeID: nodeID, Epoch: epoch}, token), 0)
			pending++
			if pending >= 1000 {
				flush()
			}
		}
	}
	flush()
}

func locateBenchNodeID(server int) string {
	return fmt.Sprintf("node-%04d", server)
}

func locateBenchRoleID(server, player int) string {
	return fmt.Sprintf("role-%04d-%06d", server, player)
}

func logActorLocateBenchMemory(b testing.TB, ctx context.Context, client *redis.Client, label string) {
	b.Helper()
	memoryInfo, err := client.Info(ctx, "memory").Result()
	if err != nil {
		b.Fatalf("read Redis memory info error = %v", err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(memoryInfo, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok {
			values[key] = value
		}
	}
	dbSize, err := client.DBSize(ctx).Result()
	if err != nil {
		b.Fatalf("read Redis db size error = %v", err)
	}
	b.Logf("%s dbsize=%d used_memory=%s used_memory_rss=%s mem_fragmentation_ratio=%s", label, dbSize, values["used_memory"], values["used_memory_rss"], values["mem_fragmentation_ratio"])
}

type locateConcurrentResult struct {
	requests       int
	errors         int
	totalLatency   []time.Duration
	commandLatency []time.Duration
	queueLatency   []time.Duration
}

type redisCommandStats struct {
	calls       float64
	usec        float64
	usecPerCall float64
}

type redisInfoSnapshot struct {
	usedCPUUser      float64
	usedCPUSys       float64
	instantaneousOps float64
	totalCommands    float64
	commandStats     map[string]redisCommandStats
}

func TestActorLocateConcurrentLoad(t *testing.T) {
	if os.Getenv("RUN_ACTOR_LOCATE_BENCH") != "1" || os.Getenv("ACTOR_LOCATE_CONCURRENCY") != "1" {
		t.Skip("set RUN_ACTOR_LOCATE_BENCH=1 and ACTOR_LOCATE_CONCURRENCY=1 to run the concurrent Redis actor locate test")
	}

	ctx := context.Background()
	client := newActorLocateBenchRedis(t)
	config := actorLocateBenchConfigFromEnv(t)
	duration := actorLocateBenchDurationEnv(t, "ACTOR_LOCATE_CONCURRENCY_DURATION", 30*time.Second)
	workers := actorLocateBenchIntEnv(t, "ACTOR_LOCATE_CONCURRENCY_WORKERS", config.poolSize)
	const randomSeed1 = uint64(0x20260903)
	const randomSeed2 = uint64(0x6c6f63617465)
	t.Logf("servers=%d players_per_server=%d total_players=%d workers=%d duration=%s random_seed=(%d,%d) redis=%s db=%d", config.servers, config.playersPerServer, config.totalPlayers(), workers, duration, randomSeed1, randomSeed2, config.addr, config.db)
	t.Cleanup(func() {
		_ = client.FlushDB(ctx).Err()
		_ = client.Close()
	})

	prefix := fmt.Sprintf("actor-locate-concurrency:%d", time.Now().UnixNano())
	rates := []int{100, 500, 1000}
	for _, layout := range []string{"ktm_server", "gserver"} {
		if err := client.FlushDB(ctx).Err(); err != nil {
			t.Fatalf("flush %s dataset error = %v", layout, err)
		}

		var lookup func(context.Context, string) error
		switch layout {
		case "ktm_server":
			data := newLegacyLocateBenchData(prefix)
			start := time.Now()
			populateLegacyLocateBenchData(t, ctx, client, config, data)
			t.Logf("layout=%s dataset_setup=%s", layout, time.Since(start))
			lookup = func(ctx context.Context, roleID string) error {
				_, err := client.Eval(ctx, locateBenchLookupScript, []string{data.rankKey}, data.onlinePrefix, data.infoPrefix, roleID).Result()
				if err == redis.Nil {
					return nil
				}
				return err
			}
		case "gserver":
			data := newCurrentLocateBenchData(prefix)
			start := time.Now()
			populateCurrentLocateBenchData(t, ctx, client, config, data)
			t.Logf("layout=%s dataset_setup=%s", layout, time.Since(start))
			lookup = func(ctx context.Context, roleID string) error {
				_, err := client.Eval(ctx, actorLocatorLocateScript, []string{data.keyPrefix + roleID}).Result()
				return err
			}
		}

		for _, rate := range rates {
			before := readActorLocateRedisInfo(t, ctx, client)
			start := time.Now()
			result := runActorLocateConcurrentLoad(ctx, lookup, config.totalPlayers(), config.playersPerServer, rate, duration, workers, randomSeed1, randomSeed2)
			elapsed := time.Since(start)
			after := readActorLocateRedisInfo(t, ctx, client)
			logActorLocateConcurrentResult(t, layout, rate, elapsed, result, before, after)
		}
	}
}

func actorLocateBenchDurationEnv(tb testing.TB, name string, fallback time.Duration) time.Duration {
	tb.Helper()
	value := actorLocateBenchStringEnv(name, fallback.String())
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		tb.Fatalf("%s must be a positive duration, got %q", name, value)
	}
	return parsed
}

func runActorLocateConcurrentLoad(ctx context.Context, lookup func(context.Context, string) error, totalPlayers, playersPerServer, rate int, duration time.Duration, workers int, seed1, seed2 uint64) locateConcurrentResult {
	result := locateConcurrentResult{
		totalLatency:   make([]time.Duration, 0, rate*int(duration/time.Second)+1),
		commandLatency: make([]time.Duration, 0, rate*int(duration/time.Second)+1),
		queueLatency:   make([]time.Duration, 0, rate*int(duration/time.Second)+1),
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	start := time.Now()
	end := start.Add(duration)
	workerInterval := time.Duration(workers) * time.Second / time.Duration(rate)
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(seed1+uint64(rate)+uint64(worker), seed2+uint64(rate)+uint64(worker)))
			next := start.Add(time.Duration(worker) * time.Second / time.Duration(rate))
			for next.Before(end) {
				if wait := time.Until(next); wait > 0 {
					time.Sleep(wait)
				}
				roleID := "role-missing"
				if rng.IntN(100) != 0 {
					player := rng.IntN(totalPlayers)
					roleID = locateBenchRoleID(player/playersPerServer, player%playersPerServer)
				}
				commandStart := time.Now()
				err := lookup(ctx, roleID)
				finished := time.Now()

				mu.Lock()
				result.requests++
				if err != nil {
					result.errors++
				}
				result.queueLatency = append(result.queueLatency, commandStart.Sub(next))
				result.commandLatency = append(result.commandLatency, finished.Sub(commandStart))
				result.totalLatency = append(result.totalLatency, finished.Sub(next))
				mu.Unlock()
				next = next.Add(workerInterval)
			}
		}(worker)
	}
	wg.Wait()
	return result
}

func readActorLocateRedisInfo(t testing.TB, ctx context.Context, client *redis.Client) redisInfoSnapshot {
	t.Helper()
	raw, err := client.Info(ctx, "cpu", "stats", "commandstats").Result()
	if err != nil {
		t.Fatalf("read Redis INFO error = %v", err)
	}
	values := make(map[string]float64)
	commandStats := make(map[string]redisCommandStats)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "cmdstat_") {
			commandStats[strings.TrimPrefix(key, "cmdstat_")] = parseRedisCommandStats(value)
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil {
			values[key] = parsed
		}
	}
	return redisInfoSnapshot{
		usedCPUUser:      values["used_cpu_user"],
		usedCPUSys:       values["used_cpu_sys"],
		instantaneousOps: values["instantaneous_ops_per_sec"],
		totalCommands:    values["total_commands_processed"],
		commandStats:     commandStats,
	}
}

func parseRedisCommandStats(value string) redisCommandStats {
	var result redisCommandStats
	for _, field := range strings.Split(value, ",") {
		key, fieldValue, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		parsed, err := strconv.ParseFloat(fieldValue, 64)
		if err != nil {
			continue
		}
		switch key {
		case "calls":
			result.calls = parsed
		case "usec":
			result.usec = parsed
		case "usec_per_call":
			result.usecPerCall = parsed
		}
	}
	return result
}

func logActorLocateConcurrentResult(t testing.TB, layout string, rate int, elapsed time.Duration, result locateConcurrentResult, before, after redisInfoSnapshot) {
	t.Helper()
	sort.Slice(result.totalLatency, func(i, j int) bool { return result.totalLatency[i] < result.totalLatency[j] })
	sort.Slice(result.commandLatency, func(i, j int) bool { return result.commandLatency[i] < result.commandLatency[j] })
	sort.Slice(result.queueLatency, func(i, j int) bool { return result.queueLatency[i] < result.queueLatency[j] })
	cpuSeconds := after.usedCPUUser + after.usedCPUSys - before.usedCPUUser - before.usedCPUSys
	cpuPercent := cpuSeconds / elapsed.Seconds() * 100
	t.Logf("layout=%s rate=%d target_qps=%d elapsed=%s requests=%d achieved_qps=%.2f errors=%d total_latency_p50=%s total_latency_p95=%s total_latency_p99=%s command_latency_p99=%s queue_latency_p99=%s redis_cpu_seconds=%.6f redis_cpu_percent=%.2f redis_ops_per_sec_end=%.0f redis_commands_delta=%.0f", layout, rate, rate, elapsed, result.requests, float64(result.requests)/elapsed.Seconds(), result.errors, durationPercentile(result.totalLatency, 0.50), durationPercentile(result.totalLatency, 0.95), durationPercentile(result.totalLatency, 0.99), durationPercentile(result.commandLatency, 0.99), durationPercentile(result.queueLatency, 0.99), cpuSeconds, cpuPercent, after.instantaneousOps, after.totalCommands-before.totalCommands)
	for _, command := range []string{"eval", "evalsha", "get", "sismember", "zrange"} {
		beforeStats := before.commandStats[command]
		afterStats := after.commandStats[command]
		t.Logf("layout=%s rate=%d command=%s calls_delta=%.0f usec_delta=%.0f usec_per_call_end=%.3f", layout, rate, command, afterStats.calls-beforeStats.calls, afterStats.usec-beforeStats.usec, afterStats.usecPerCall)
	}
}

func durationPercentile(values []time.Duration, percentile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * percentile)
	return values[index]
}
