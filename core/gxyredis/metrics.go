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
		gxymetrics.RedisRequestDuration.WithLabelValues(cmd.FullName()).Observe(time.Since(start).Seconds())
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
		gxymetrics.RedisRequestDuration.WithLabelValues("pipeline").Observe(time.Since(start).Seconds())
		if err != nil {
			gxymetrics.RedisErrors.WithLabelValues("pipeline").Inc()
		}
		return err
	}
}
