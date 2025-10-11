package gxymongo

import (
	"context"
	"fmt"
	"gserver/core/gxylog"

	"github.com/gogf/gf/v2/os/glog"
)

type mongoLogger gxylog.LogAdapter

func NewMongoLogger() *mongoLogger {
	return (*mongoLogger)(gxylog.NewLogAdapter(context.Background(), "mongo", glog.LEVEL_ERRO))
}

func (l *mongoLogger) formatMsg(message string, v ...any) string {
	log := fmt.Sprintf("message:%s", message)
	for i := 0; i < len(v); i += 2 {
		key := fmt.Sprintf("%s", v[i])
		value := fmt.Sprintf("%s", v[i+1])
		log = fmt.Sprintf("%s, %s:%s", log, key, value)
	}
	return log
}

func (l *mongoLogger) Info(level int, message string, v ...any) {
	switch level {
	case 0: // off
		return
	case 1: // info
		if l.Level >= glog.LEVEL_INFO {
			l.Logger.Info(l.Ctx, l.formatMsg(message, v...))
		}
	case 2: // debug
		if l.Level >= glog.LEVEL_DEBU {
			l.Logger.Debug(l.Ctx, l.formatMsg(message, v...))
		}
	default:
		l.Logger.Info(l.Ctx, l.formatMsg(message, v...))
	}
}

func (l *mongoLogger) Error(err error, message string, v ...any) {
	v = append(v, "error", err.Error())
	l.Logger.Error(l.Ctx, l.formatMsg(message, v...))
}
