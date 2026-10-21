package gxyredis

import (
	"context"
	"errors"
	"time"

	"gserver/core/gxymetrics"

	"github.com/redis/go-redis/v9"
)

type metricsHook struct{}

func (h metricsHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h metricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmd)
		// 带 exemplar:标签只有命令名(eval 混着归属仲裁的 5 个脚本),
		// exemplar 能直接把慢样本指到具体 trace。
		gxymetrics.ObserveWithTrace(ctx, gxymetrics.RedisRequestDuration.WithLabelValues(cmd.FullName()), time.Since(start).Seconds())
		if err != nil && !errors.Is(err, redis.Nil) {
			// redis.Nil 是"键不存在"这种正常结果,不是故障。
			gxymetrics.RedisErrors.WithLabelValues(cmd.FullName()).Inc()
		}
		return err
	}
}

func (h metricsHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()
		err := next(ctx, cmds)
		gxymetrics.ObserveWithTrace(ctx, gxymetrics.RedisRequestDuration.WithLabelValues("pipeline"), time.Since(start).Seconds())
		if err != nil {
			gxymetrics.RedisErrors.WithLabelValues("pipeline").Inc()
		}
		return err
	}
}
