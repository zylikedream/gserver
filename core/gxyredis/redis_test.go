package gxyredis

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func TestGet(t *testing.T) {
	if os.Getenv("RUN_REDIS_TESTS") != "1" {
		t.Skip("set RUN_REDIS_TESTS=1 to run Redis integration tests")
	}
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("config/redis.test.toml")
	ctx := context.Background()
	redisApp := NewRedisApp()
	if err := redisApp.OnModInit(ctx); err != nil {
		t.Skipf("redis test config unavailable: %v", err)
	}
	if redisApp.client == nil {
		t.Skip("redis test config has no redis.addr")
	}
	if err := redisApp.OnModStart(ctx); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	t.Cleanup(func() {
		_ = redisApp.OnModStop(ctx)
	})

	key := "test_key"
	value := "test_value"
	redisCli := redisApp.client
	if err := redisCli.Set(ctx, key, value, 0).Err(); err != nil {
		t.Fatalf("failed to set key %s in Redis: %v", key, err)
	}

	result, err := redisCli.Get(ctx, key).Result()
	if err != nil {
		t.Errorf("Failed to get key %s from Redis, error:%v", key, err)
	}

	if result != value {
		t.Errorf("Expected value %s, but got %s", value, result)
	}
	if _, err = redisCli.Get(ctx, "not_exist").Result(); err != nil && !errors.Is(err, redis.Nil) {
		t.Errorf("Failed to get key %s from Redis, error:%v", key, err)
	}
}
