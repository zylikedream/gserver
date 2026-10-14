package gxylock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	mathrand "math/rand"
	"slices"
	"sort"
	"time"

	"github.com/cockroachdb/errors"

	"gserver/core/gxylog"

	"github.com/redis/go-redis/v9"
)

const (
	defaultAcquireAttempts = 3
	defaultRetryBaseDelay  = 30 * time.Millisecond
	// releaseTimeout 是释放锁时给 redis 的上限,仅用于防止释放路径无限挂起
	releaseTimeout = time.Second
)

var ErrBusy = errors.New("lock busy")

type Manager interface {
	Acquire(ctx context.Context, key string, ttl time.Duration) (string, bool, error)
	Release(ctx context.Context, key string, token string)
}

type RedisManager struct {
	ClientProvider  func() redis.UniversalClient
	AcquireAttempts int
	RetryBaseDelay  time.Duration
}

func NewRedisManager(provider func() redis.UniversalClient) *RedisManager {
	return &RedisManager{
		ClientProvider:  provider,
		AcquireAttempts: defaultAcquireAttempts,
		RetryBaseDelay:  defaultRetryBaseDelay,
	}
}

func (m *RedisManager) Acquire(ctx context.Context, key string, ttl time.Duration) (string, bool, error) {
	client := m.client()
	if client == nil {
		return "", false, errors.New("redis lock client not initialized")
	}

	token, err := newToken()
	if err != nil {
		return "", false, err
	}

	attempts := m.acquireAttempts()
	for attempt := range attempts {
		ok, err := client.SetNX(ctx, key, token, ttl).Result()
		if err != nil {
			return "", false, err
		}
		if ok {
			return token, true, nil
		}
		if attempt == attempts-1 {
			break
		}
		if err := sleepBeforeRetry(ctx, m.retryBaseDelay(), attempt); err != nil {
			return "", false, err
		}
	}
	return "", false, nil
}

func (m *RedisManager) Release(ctx context.Context, key string, token string) {
	client := m.client()
	if client == nil {
		return
	}
	// 脚本返回 1 表示确实删掉了自己持有的锁,0 表示 key 已不在或 token 不匹配。
	// 后者是"锁在持有期间已经丢了"的唯一外部可观测点(TTL 过期、被清理等),
	// 一旦发生就说明临界区可能被并发进入,必须留痕。
	res, err := releaseScript.Run(ctx, client, []string{key}, token).Int64()
	if err != nil {
		gxylog.Warn(ctx, "release lock failed", gxylog.Str("key", key), gxylog.Err(err))
		return
	}
	if res != 1 {
		gxylog.Warn(ctx, "release lock skipped: not the owner or already expired",
			gxylog.Str("key", key), gxylog.Num("result", res))
	}
}

func (m *RedisManager) client() redis.UniversalClient {
	if m == nil || m.ClientProvider == nil {
		return nil
	}
	return m.ClientProvider()
}

func (m *RedisManager) acquireAttempts() int {
	if m == nil || m.AcquireAttempts <= 0 {
		return defaultAcquireAttempts
	}
	return m.AcquireAttempts
}

func (m *RedisManager) retryBaseDelay() time.Duration {
	if m == nil || m.RetryBaseDelay <= 0 {
		return defaultRetryBaseDelay
	}
	return m.RetryBaseDelay
}

// With 在 keys 上依次加锁(去重后按字典序,防死锁),全程持有并执行 fn,返回后释放。
//
// ttl 的约束必须由调用方保证:**ttl 只覆盖 fn 的执行期,本函数不续期** ——
// fn 耗时超过 ttl 时锁会在 redis 里自行过期,互斥静默失效(没有错误、没有日志),
// 另一个调用方会同时进入临界区。所以 ttl 要按 fn 的最坏耗时(含 DB 卡顿、
// 调度延迟)留足余量,不能只按正常路径估计。
//
// 任一键取不到即逆序释放已持有的锁并返回 ErrBusy,fn 不执行。
//
// heldLock 一把已持有的锁:键与本次持有的 token。
type heldLock struct {
	// key 是锁键,token 是本次持有的凭据(释放时比对,避免释放别人的锁)
	key   string
	token string
}

func With(ctx context.Context, mgr Manager, keys []string, ttl time.Duration, fn func() error) error {
	if fn == nil {
		return nil
	}
	keys = uniqueSortedStrings(keys)
	if len(keys) == 0 {
		return fn()
	}

	held := make([]heldLock, 0, len(keys))
	for _, key := range keys {
		token, ok, err := mgr.Acquire(ctx, key, ttl)
		if err != nil {
			releaseHeld(ctx, mgr, held)
			return err
		}
		if !ok {
			releaseHeld(ctx, mgr, held)
			// 哨兵直接返回,不补栈:cockroachdb 的 New 已带定义处栈,
			// 再 WithStack 会挂两层栈(见 docs/development/error-handling.md 第 6 条)
			return ErrBusy
		}
		held = append(held, heldLock{key: key, token: token})
	}
	defer releaseHeld(ctx, mgr, held)
	return fn()
}

var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

// releaseHeld 逆序释放已持有的锁。
// 用脱离取消的 ctx:With 的 ctx 通常就是请求 ctx,超时/关服时已取消,直接透传会
// 让释放命令发不出去、锁一直悬到 TTL —— 与"返回后释放"的契约相悖。
// Release 本身不返回错误,故这里也没有可上报的失败。
func releaseHeld(ctx context.Context, mgr Manager, held []heldLock) {
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), releaseTimeout)
	defer cancel()

	for _, h := range slices.Backward(held) {
		mgr.Release(rctx, h.key, h.token)
	}
}

func newToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func sleepBeforeRetry(ctx context.Context, baseDelay time.Duration, attempt int) error {
	if baseDelay <= 0 {
		return nil
	}
	delay := baseDelay * time.Duration(1<<attempt)
	jitter := time.Duration(mathrand.Int63n(int64(baseDelay)))
	timer := time.NewTimer(delay + jitter)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
