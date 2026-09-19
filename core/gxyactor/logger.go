package gxyactor

import (
	"context"
	"fmt"

	"gserver/core/gxylog"

	"ergo.services/ergo/gen"
)

// ergoLogger 把运行时的日志接入 gxylog。
// 用于替换默认 logger(默认 logger 必须显式关闭,否则会重复输出)。
type ergoLogger struct {
	ctx context.Context
}

func newErgoLogger() *ergoLogger {
	return &ergoLogger{
		ctx: gxylog.NewContext(context.Background(), "actor_sys"),
	}
}

func (l *ergoLogger) Log(message gen.MessageLog) {
	var fields []gxylog.Field

	fields = append(fields, gxylog.Str("source", sourceName(message.Source)))
	if message.Level != gen.LogLevelDefault {
		fields = append(fields, gxylog.Str("level", message.Level.String()))
	}
	for _, f := range message.Fields {
		fields = append(fields, gxylog.Any(f.Name, f.Value))
	}

	msg := message.Format
	if len(message.Args) > 0 {
		msg = fmt.Sprintf(message.Format, message.Args...)
	}

	switch message.Level {
	case gen.LogLevelSystem, gen.LogLevelTrace, gen.LogLevelDebug:
		gxylog.Debug(l.ctx, msg, fields...)
	case gen.LogLevelWarning:
		gxylog.Warn(l.ctx, msg, fields...)
	case gen.LogLevelError:
		gxylog.Error(l.ctx, msg, fields...)
	case gen.LogLevelDisabled:
		return
	default:
		// default / info / panic 走 Info。
		// LogLevelPanic 表示"框架捕获了进程内的 panic 并已终止该进程",
		// 不是致命错误:映射到 gxylog.Fatal 会 os.Exit(1),让单个 actor 的 panic
		// 终止整个进程(见 ADR 0013)。
		gxylog.Info(l.ctx, msg, fields...)
	}
}

// Terminate 在 logger 被移除或节点停止时调用。当前无需清理。
func (l *ergoLogger) Terminate() {}

// sourceName 把日志来源转成可读标识。
func sourceName(source any) string {
	switch s := source.(type) {
	case gen.MessageLogProcess:
		return fmt.Sprintf("process:%s/%s", s.Name, s.Behavior)
	case gen.MessageLogNode:
		return "node"
	case gen.MessageLogNetwork:
		return "network"
	case gen.MessageLogMeta:
		return fmt.Sprintf("meta:%s", s.Meta)
	default:
		return "unknown"
	}
}
