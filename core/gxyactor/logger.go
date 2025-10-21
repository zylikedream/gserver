package gxyactor

import (
	"context"
	"gserver/core/gxylog"
	"log/slog"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/os/glog"
)

// enable Zap logging
func glogAdapterLogging(system *actor.ActorSystem) *slog.Logger {
	handler := (*actorLogAdapter)(gxylog.NewLogAdapter(context.Background(), "actor_sys", glog.LEVEL_DEBU))
	return slog.New(handler).
		With("lib", "Proto.Actor").
		With("system", system.ID)
}

type actorLogAdapter gxylog.LogAdapter

func slevel2glevel(level slog.Level) int {
	switch level {
	case slog.LevelDebug:
		return glog.LEVEL_DEBU
	case slog.LevelInfo:
		return glog.LEVEL_INFO
	case slog.LevelWarn:
		return glog.LEVEL_WARN
	case slog.LevelError:
		return glog.LEVEL_ERRO
	default:
		return glog.LEVEL_INFO
	}
}

// 简化Enabled方法实现，因为我们不完全确定glog的level API
func (g *actorLogAdapter) Enabled(_ context.Context, r slog.Level) bool {
	return slevel2glevel(r) >= g.Level
}

func (g *actorLogAdapter) Handle(ctx context.Context, r slog.Record) error {
	// 构建日志消息和字段
	var fields []interface{}
	fields = append(fields, r.Message)

	r.Attrs(func(a slog.Attr) bool {
		fields = append(fields, a.Key, a.Value.Any())
		return true
	})

	// 根据不同的日志级别调用不同的glog方法
	switch r.Level {
	case slog.LevelDebug:
		g.Logger.Debug(g.Ctx, fields...)
	case slog.LevelInfo:
		g.Logger.Info(g.Ctx, fields...)
	case slog.LevelWarn:
		g.Logger.Warning(g.Ctx, fields...)
	case slog.LevelError:
		g.Logger.Error(g.Ctx, fields...)
	default:
		g.Logger.Info(g.Ctx, fields...)
	}
	return nil
}

func (g *actorLogAdapter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return g
}

func (g *actorLogAdapter) WithGroup(name string) slog.Handler {
	return g
}
