package gxyredis

import (
	"context"
	"errors"
	"slices"
	"testing"

	"gserver/core/gxymetrics"
	"gserver/core/gxytrace"

	"ergo.services/ergo/gen"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
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

// TestProcessHookAttachesExemplar Redis 指标的标签只有命令名(eval 混着归属仲裁的
// 5 个脚本):慢样本必须带 trace_id,才能定位到具体 trace。
func TestProcessHookAttachesExemplar(t *testing.T) {
	ctx := gxytrace.ContextWithErgoTrace(context.Background(),
		gen.Tracing{ID: [2]uint64{0x3333, 0x4444}, SpanID: 5})
	want := gxymetrics.TraceIDFromContext(ctx)
	if want == "" {
		t.Fatal("测试上下文必须带 trace_id")
	}

	cmd := redis.NewStringCmd(context.Background(), "SMOKE_EXEMPLAR")
	hook := metricsHook{}
	next := func(context.Context, redis.Cmder) error { return nil }
	if err := hook.ProcessHook(next)(ctx, cmd); err != nil {
		t.Fatalf("hook: %v", err)
	}

	var value dto.Metric
	if err := gxymetrics.RedisRequestDuration.WithLabelValues(cmd.FullName()).(interface {
		Write(*dto.Metric) error
	}).Write(&value); err != nil {
		t.Fatalf("write redis histogram: %v", err)
	}
	var ids []string
	for _, bucket := range value.GetHistogram().GetBucket() {
		if exemplar := bucket.GetExemplar(); exemplar != nil {
			for _, label := range exemplar.GetLabel() {
				if label.GetName() == "trace_id" {
					ids = append(ids, label.GetValue())
				}
			}
		}
	}
	if !slices.Contains(ids, want) {
		t.Fatalf("样本未带 exemplar trace_id=%s(实际 %v)", want, ids)
	}
}
