package gxyactor

import (
	"context"
	"gserver/core/gxylog"
	"log/slog"

	"github.com/asynkron/protoactor-go/actor"
)

// protoactor 系统日志接入 gxylog(zap)
func glogAdapterLogging(system *actor.ActorSystem) *slog.Logger {
	handler := (*actorLogAdapter)(gxylog.NewLogAdapter(context.Background(), "actor_sys", gxylog.LevelError))
	return slog.New(handler).
		With("lib", "Proto.Actor").
		With("system", system.ID)
}

type actorLogAdapter gxylog.LogAdapter

func slevel2glevel(level slog.Level) int {
	switch level {
	case slog.LevelDebug:
		return gxylog.LevelDebug
	case slog.LevelInfo:
		return gxylog.LevelInfo
	case slog.LevelWarn:
		return gxylog.LevelWarn
	case slog.LevelError:
		return gxylog.LevelError
	default:
		return gxylog.LevelInfo
	}
}

func (g *actorLogAdapter) Enabled(_ context.Context, r slog.Level) bool {
	return slevel2glevel(r) >= g.Level
}

func (g *actorLogAdapter) Handle(ctx context.Context, r slog.Record) error {
	var fields []gxylog.Field
	r.Attrs(func(a slog.Attr) bool {
		fields = append(fields, gxylog.Any(a.Key, a.Value.Any()))
		return true
	})

	switch r.Level {
	case slog.LevelDebug:
		gxylog.Debug(g.Ctx, r.Message, fields...)
	case slog.LevelInfo:
		gxylog.Info(g.Ctx, r.Message, fields...)
	case slog.LevelWarn:
		gxylog.Warn(g.Ctx, r.Message, fields...)
	case slog.LevelError:
		gxylog.Error(g.Ctx, r.Message, fields...)
	default:
		gxylog.Info(g.Ctx, r.Message, fields...)
	}
	return nil
}

func (g *actorLogAdapter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return g
}

func (g *actorLogAdapter) WithGroup(name string) slog.Handler {
	return g
}
