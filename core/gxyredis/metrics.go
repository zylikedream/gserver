package gxyredis

import (
	"context"
	"errors"
	"time"

	"gserver/core/gxymetrics"
	"gserver/core/gxytrace"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type metricsHook struct{}

func (h metricsHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h metricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		// 一条命令一个 Client span;只在被采样的链路里产生(规则见 StartClientSpan)。
		ctx, span := gxytrace.StartClientSpan(ctx, "redis."+cmd.FullName(),
			attribute.String("db.system", "redis"))
		defer span.End()

		start := time.Now()
		err := next(ctx, cmd)
		// 带 exemplar:标签只有命令名(eval 混着归属仲裁的 5 个脚本),
		// exemplar 能直接把慢样本指到具体 trace。
		gxymetrics.ObserveWithTrace(ctx, gxymetrics.RedisRequestDuration.WithLabelValues(cmd.FullName()), time.Since(start).Seconds())
		if err != nil {
			if errors.Is(err, redis.Nil) {
				// redis.Nil 是"键不存在"这种正常结果,不是故障。
				return err
			}
			gxymetrics.RedisErrors.WithLabelValues(cmd.FullName()).Inc()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return err
	}
}

func (h metricsHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		ctx, span := gxytrace.StartClientSpan(ctx, "redis.pipeline",
			attribute.String("db.system", "redis"))
		defer span.End()

		start := time.Now()
		err := next(ctx, cmds)
		gxymetrics.ObserveWithTrace(ctx, gxymetrics.RedisRequestDuration.WithLabelValues("pipeline"), time.Since(start).Seconds())
		if err != nil {
			gxymetrics.RedisErrors.WithLabelValues("pipeline").Inc()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return err
	}
}
