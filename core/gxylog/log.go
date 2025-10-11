package gxylog

import (
	"context"
	"fmt"

	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/gogf/gf/v2/os/gfile"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gconv"
)

const (
	ContextKeyModType = "mod"
	ContextKeyRoleID  = "roleID"
)

var logger = glog.DefaultLogger()

func InitLog(ctx context.Context, config string, logType string) error {
	logger.SetFlags(glog.F_FILE_SHORT | glog.F_TIME_STD)
	cfg := gcfg.Instance(config)

	if cfg != nil {
		conf := cfg.MustData(ctx)
		if err := glog.SetConfigWithMap(conf); err != nil {
			return err
		}
	}
	logger.AppendCtxKeys(ContextKeyModType, ContextKeyRoleID)
	logger.SetPath(gfile.Join(logger.GetConfig().Path, logType))
	logger.SetFile(fmt.Sprintf("%s_%s", logType, logger.GetConfig().File))
	return nil
}

func WithValue(ctx context.Context, key string, value any) context.Context {
	gkey := gctx.StrKey(key)
	return context.WithValue(ctx, gkey, fmt.Sprintf("%s:%s", gkey, gconv.String(value)))
}

func NewContext(ctx context.Context, mod string) context.Context {
	return WithValue(ctx, ContextKeyModType, mod)
}

func GetLogger() *glog.Logger {
	return logger
}

type LogAdapter struct {
	Logger *glog.Logger
	Ctx    context.Context
	Level  int
}

func NewLogAdapter(ctx context.Context, typ string, level int) *LogAdapter {
	return &LogAdapter{
		Logger: logger,
		Ctx:    NewContext(ctx, typ),
		Level:  level,
	}
}
