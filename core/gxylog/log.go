package gxylog

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
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

func InitLog(ctx context.Context, logType string) error {
	logger.SetFlags(glog.F_FILE_SHORT | glog.F_TIME_STD)
	conf := g.Cfg().MustGet(ctx, "log").Map()
	if err := glog.SetConfigWithMap(conf); err != nil {
		return err
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
		Logger: logger.Skip(1),
		Ctx:    NewContext(ctx, typ),
		Level:  level,
	}
}

// --- Structured Logging ---

type Field struct {
	Key   string
	Value string
}

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64
}

func Str(key, val string) Field { return Field{key, val} }
func Num[T Number](key string, val T) Field {
	return Field{key, fmt.Sprintf("%d", val)}
}
func Bool(key string, val bool) Field {
	return Field{key, strconv.FormatBool(val)}
}
func Err(err error) Field {
	return Field{"error", fmt.Sprintf("%+v", err)}
}
func Any(key string, val any) Field {
	return Field{key, fmt.Sprintf("%v", val)}
}

func formatFields(msg string, fields []Field) string {
	if len(fields) == 0 {
		return msg
	}
	var b strings.Builder
	b.WriteString(msg)
	var errField Field
	for _, f := range fields {
		if f.Key == "error" {
			errField = f
			continue
		}
		b.WriteString(", ")
		b.WriteString(f.Key)
		b.WriteByte('=')
		b.WriteString(f.Value)
	}
	// errField 最后一个
	if errField.Key != "" {
		b.WriteString(", ")
		b.WriteString(errField.Key)
		b.WriteByte('=')
		b.WriteString(errField.Value)
	}
	return b.String()
}

func Info(ctx context.Context, msg string, fields ...Field) {
	logger.Skip(1).Info(ctx, formatFields(msg, fields))
}
func Debug(ctx context.Context, msg string, fields ...Field) {
	logger.Skip(1).Debug(ctx, formatFields(msg, fields))
}
func Warn(ctx context.Context, msg string, fields ...Field) {
	logger.Skip(1).Warning(ctx, formatFields(msg, fields))
}
func Error(ctx context.Context, msg string, fields ...Field) {
	logger.Skip(1).Error(ctx, formatFields(msg, fields))
}
func Fatal(ctx context.Context, msg string, fields ...Field) {
	logger.Skip(1).Fatal(ctx, formatFields(msg, fields))
}
