package logger

import (
	"context"
	"gserver/core/gxylog"

	"github.com/gogf/gf/v2/os/glog"
)

type gnetLogAdapter gxylog.LogAdapter

func NewGnetLogger() *gnetLogAdapter {
	return (*gnetLogAdapter)(gxylog.NewLogAdapter(context.Background(), "gnet", glog.LEVEL_ERRO))
}

func (l *gnetLogAdapter) Debugf(format string, v ...any) {
	if l.Level <= glog.LEVEL_DEBU {
		l.Logger.Debugf(l.Ctx, format, v...)
	}

}

func (l *gnetLogAdapter) Infof(format string, v ...any) {
	if l.Level <= glog.LEVEL_INFO {
		l.Logger.Infof(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Warnf(format string, v ...any) {
	if l.Level <= glog.LEVEL_WARN {
		l.Logger.Warningf(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Errorf(format string, v ...any) {
	if l.Level <= glog.LEVEL_ERRO {
		l.Logger.Errorf(l.Ctx, format, v...)
	}
}

func (l *gnetLogAdapter) Fatalf(format string, v ...any) {
	if l.Level <= glog.LEVEL_FATA {
		l.Logger.Fatalf(l.Ctx, format, v...)
	}
}
