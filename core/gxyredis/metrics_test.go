package gxyredis

import (
	"context"
	"errors"
	"testing"

	"gserver/core/gxymetrics"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"
)

// TestMetricsHookCountsErrors 失败的命令必须计入 redis_errors_total:
// 耗时直方图里成功与失败混在一起,单看它算不出失败率。
func TestMetricsHookCountsErrors(t *testing.T) {
	// 用一个不存在的命令名,避免与真实命令的计数器互相干扰。
	cmd := redis.NewStringCmd(context.Background(), "SMOKE_FAIL")
	label := cmd.FullName()
	before := testutil.ToFloat64(gxymetrics.RedisErrors.WithLabelValues(label))

	hook := metricsHook{}
	next := func(context.Context, redis.Cmder) error { return errors.New("boom") }
	if err := hook.ProcessHook(next)(context.Background(), cmd); err == nil {
		t.Fatal("hook 必须把错误原样返回")
	}

	if got := testutil.ToFloat64(gxymetrics.RedisErrors.WithLabelValues(label)); got != before+1 {
		t.Fatalf("错误计数 = %v, want %v", got, before+1)
	}
}

// TestMetricsHookIgnoresRedisNil redis.Nil 是"键不存在"的正常结果,不能算故障。
func TestMetricsHookIgnoresRedisNil(t *testing.T) {
	cmd := redis.NewStringCmd(context.Background(), "SMOKE_MISS")
	label := cmd.FullName()
	before := testutil.ToFloat64(gxymetrics.RedisErrors.WithLabelValues(label))

	hook := metricsHook{}
	next := func(context.Context, redis.Cmder) error { return redis.Nil }
	_ = hook.ProcessHook(next)(context.Background(), cmd)

	if got := testutil.ToFloat64(gxymetrics.RedisErrors.WithLabelValues(label)); got != before {
		t.Fatalf("redis.Nil 不应计入错误: %v -> %v", before, got)
	}
}
