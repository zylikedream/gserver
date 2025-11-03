package gxyredis

import (
	"context"
	"testing"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
)

func TestGet(t *testing.T) {
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName("config/redis.test.toml")
	ctx := context.Background()
	redisCli := newRedisApp()
	key := "test_key"
	value := "test_value"
	redisCli.Set(ctx, key, value, 0)

	result, err := redisCli.Get(ctx, "1").Result()
	if err != nil {
		t.Errorf("Failed to get key %s from Redis, error:%v", key, err)
	}

	if result != value {
		t.Errorf("Expected value %s, but got %s", value, result)
	}
	_, err = redisCli.Exists(ctx, "not_exist").Result()
	if err != nil {
		t.Errorf("Failed to get key %s from Redis, error:%v", key, err)
	}
}
