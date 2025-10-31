package gxyredis

import (
	"context"
	"testing"
)

func TestGet(t *testing.T) {
	ctx := context.Background()
	redisCli := NewRedisClient("config/redis.test.toml")
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
