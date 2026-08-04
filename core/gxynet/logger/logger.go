package logger

import (
	"context"
	"gserver/core/gxylog"
)

type gnetLogAdapter gxylog.LogAdapter

func NewGnetLogger() *gnetLogAdapter {
	return (*gnetLogAdapter)(gxylog.NewLogAdapter(context.Background(), "gnet", gxylog.LevelError))
}

func (l *gnetLogAdapter) Debugf(format string, v ...any) {
	if l.Level <= gxylog.LevelDebug {
		gxylog.Debugf(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Infof(format string, v ...any) {
	if l.Level <= gxylog.LevelInfo {
		gxylog.Infof(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Warnf(format string, v ...any) {
	if l.Level <= gxylog.LevelWarn {
		gxylog.Warnf(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Errorf(format string, v ...any) {
	if l.Level <= gxylog.LevelError {
		gxylog.Errorf(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Fatalf(format string, v ...any) {
	if l.Level <= gxylog.LevelFatal {
		gxylog.Fatalf(l.Ctx, format, v...)
	}
}
