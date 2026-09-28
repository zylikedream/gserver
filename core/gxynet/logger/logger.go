package logger

import (
	"context"
	"gserver/core/gxylog"
)

type gnetLogAdapter gxylog.LogAdapter

func NewGnetLogger() *gnetLogAdapter {
	return (*gnetLogAdapter)(gxylog.NewLogAdapter(context.Background(), "gnet", gxylog.LEVEL_ERROR))
}

func (l *gnetLogAdapter) Debugf(format string, v ...any) {
	if l.Level <= gxylog.LEVEL_DEBUG {
		gxylog.Debugf(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Infof(format string, v ...any) {
	if l.Level <= gxylog.LEVEL_INFO {
		gxylog.Infof(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Warnf(format string, v ...any) {
	if l.Level <= gxylog.LEVEL_WARN {
		gxylog.Warnf(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Errorf(format string, v ...any) {
	if l.Level <= gxylog.LEVEL_ERROR {
		gxylog.Errorf(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Fatalf(format string, v ...any) {
	if l.Level <= gxylog.LEVEL_FATAL {
		gxylog.Fatalf(l.Ctx, format, v...)
	}
}
