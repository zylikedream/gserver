// Package gxylog 是全局结构化日志入口(zap 后端:JSON 文件 + console 双输出)。
//
// 字段命名约定(违反会在 JSON 中产生重复 key 或字段被防御性拦截):
//   - 消息载荷/内容类字段统一用 payload,禁止用 msg(与日志消息主体冲突)
//   - 禁止使用内置 key:ts/level/caller/msg/stacktrace/trace_id/ctx_id
//     (trace_id/ctx_id 由 ctx 自动注入,业务无需也不应显式传)
//   - 业务字段通过 Str/Num/Bool/Err/Any 构造,随日志调用传入
package gxylog

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/gogf/gf/v2/os/gfile"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gconv"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	ContextKeyModType = "mod"
	ContextKeyRoleID  = "roleID"
)

// Level 常量(替代 goframe glog.LEVEL_*),供 LogAdapter 阈值比较使用
const (
	LevelDebug = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

var (
	logger *zap.Logger
	level  = zap.NewAtomicLevelAt(zapcore.InfoLevel)
)

// InitLog 初始化 zap logger:文件输出 JSON 行(lumberjack 轮转),stdout 输出 console 格式。
// 同时把 goframe 默认 logger 静音(g.Cfg 等内部组件杂音),http 访问日志走独立 glog 实例不受影响。
func InitLog(ctx context.Context, logType string) error {
	glog.SetDefaultLogger(glog.NewWithWriter(io.Discard))

	conf := g.Cfg().MustGet(ctx, "log").Map()
	level = zap.NewAtomicLevelAt(parseLevel(gconv.String(conf["level"])))

	path := gconv.String(conf["path"])
	if path == "" {
		path = "log"
	}
	logDir := gfile.Join(path, logType)
	if err := gfile.Mkdir(logDir); err != nil {
		return err
	}

	fileWriter := &lumberjack.Logger{
		Filename:   gfile.Join(logDir, logType+".log"),
		MaxSize:    100, // MB
		MaxBackups: 7,
		Compress:   true,
	}
	fileCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(jsonEncoderConfig()),
		zapcore.AddSync(fileWriter),
		level,
	)
	consoleCore := zapcore.NewCore(
		zapcore.NewConsoleEncoder(consoleEncoderConfig()),
		zapcore.AddSync(os.Stdout),
		level,
	)

	logger = zap.New(zapcore.NewTee(fileCore, consoleCore), zap.AddCaller())
	return nil
}

func WithValue(ctx context.Context, key string, value any) context.Context {
	return context.WithValue(ctx, gctx.StrKey(key), value)
}

func NewContext(ctx context.Context, mod string) context.Context {
	return WithValue(ctx, ContextKeyModType, mod)
}

// SetLevel 动态调整全局日志级别("all"/"debug"/"info"/"warn"/"error"/"fatal")
func SetLevel(l string) {
	level.SetLevel(parseLevel(l))
}

// LogAdapter 适配第三方库(protoactor/gnet)的日志接入,阈值比较用 Level 常量
type LogAdapter struct {
	Ctx   context.Context
	Level int
}

func NewLogAdapter(ctx context.Context, typ string, level int) *LogAdapter {
	return &LogAdapter{
		Ctx:   NewContext(ctx, typ),
		Level: level,
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

func parseLevel(l string) zapcore.Level {
	switch strings.ToLower(l) {
	case "all", "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

// reservedKeys 与 JSON encoder 固有键冲突的字段名,跳过避免 JSON 重复键。
// 业务侧已约定:消息载荷用 payload,显式 trace_id 由自动注入替代;
// 此处保留 msg/trace_id 为防御性拦截,防止未来调用点误用回归重复键。
var reservedKeys = map[string]struct{}{
	"ts": {}, "level": {}, "caller": {}, "msg": {}, "stacktrace": {},
	"trace_id": {}, "ctx_id": {},
}

// toZapFields 转换业务字段并注入 ctx 字段(mod/roleID/trace_id 或 ctx_id)
func toZapFields(ctx context.Context, fields []Field) []zap.Field {
	zs := make([]zap.Field, 0, len(fields)+3)
	for _, f := range fields {
		if _, ok := reservedKeys[f.Key]; ok {
			continue
		}
		zs = append(zs, zap.String(f.Key, f.Value))
	}
	if mod := ctxValue(ctx, ContextKeyModType); mod != "" {
		zs = append(zs, zap.String(ContextKeyModType, mod))
	}
	if rid := ctxValue(ctx, ContextKeyRoleID); rid != "" {
		zs = append(zs, zap.String(ContextKeyRoleID, rid))
	}
	// traceID:仅当 span 被采样时注入(未采样的 span 无对应 trace,打了会导致跳转 404);
	// 无有效 span 时兜底 goframe CtxId
	if sc := trace.SpanFromContext(ctx).SpanContext(); sc.IsValid() && sc.IsSampled() && sc.TraceID().IsValid() {
		zs = append(zs, zap.String("trace_id", sc.TraceID().String()))
	} else if cid := gctx.CtxId(ctx); cid != "" {
		zs = append(zs, zap.String("ctx_id", cid))
	}
	return zs
}

func ctxValue(ctx context.Context, key string) string {
	if v := ctx.Value(gctx.StrKey(key)); v != nil {
		return gconv.String(v)
	}
	return ""
}

// logf 统一出口;AddCallerSkip(2) 跳过 logf 与包装函数,指向业务调用点
func logf(ctx context.Context, lvl zapcore.Level, msg string, fields []Field) {
	if logger == nil {
		// InitLog 之前:stderr 兜底,保证 Fatal 仍然退出
		fmt.Fprintf(os.Stderr, "[%s] %s\n", lvl.String(), msg)
		if lvl == zapcore.FatalLevel {
			os.Exit(1)
		}
		return
	}
	l := logger.WithOptions(zap.AddCallerSkip(2))
	if ce := l.Check(lvl, msg); ce != nil {
		ce.Write(toZapFields(ctx, fields)...)
		// Check+Write 不触发 zap 的 os.Exit(仅在 Logger.Fatal 方法内),此处手动补
		if lvl == zapcore.FatalLevel {
			os.Exit(1)
		}
	}
}

func Info(ctx context.Context, msg string, fields ...Field) {
	logf(ctx, zapcore.InfoLevel, msg, fields)
}
func Debug(ctx context.Context, msg string, fields ...Field) {
	logf(ctx, zapcore.DebugLevel, msg, fields)
}
func Warn(ctx context.Context, msg string, fields ...Field) {
	logf(ctx, zapcore.WarnLevel, msg, fields)
}
func Error(ctx context.Context, msg string, fields ...Field) {
	logf(ctx, zapcore.ErrorLevel, msg, fields)
}
func Fatal(ctx context.Context, msg string, fields ...Field) {
	logf(ctx, zapcore.FatalLevel, msg, fields)
}

// 格式化变体(gnet 等库使用)
func Debugf(ctx context.Context, format string, args ...any) {
	logf(ctx, zapcore.DebugLevel, fmt.Sprintf(format, args...), nil)
}
func Infof(ctx context.Context, format string, args ...any) {
	logf(ctx, zapcore.InfoLevel, fmt.Sprintf(format, args...), nil)
}
func Warnf(ctx context.Context, format string, args ...any) {
	logf(ctx, zapcore.WarnLevel, fmt.Sprintf(format, args...), nil)
}
func Errorf(ctx context.Context, format string, args ...any) {
	logf(ctx, zapcore.ErrorLevel, fmt.Sprintf(format, args...), nil)
}
func Fatalf(ctx context.Context, format string, args ...any) {
	logf(ctx, zapcore.FatalLevel, fmt.Sprintf(format, args...), nil)
}

func jsonEncoderConfig() zapcore.EncoderConfig {
	cfg := zap.NewProductionEncoderConfig()
	cfg.TimeKey = "ts"
	cfg.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	cfg.LevelKey = "level"
	cfg.EncodeLevel = zapcore.LowercaseLevelEncoder
	cfg.CallerKey = "caller"
	cfg.EncodeCaller = zapcore.ShortCallerEncoder
	cfg.MessageKey = "msg"
	return cfg
}

func consoleEncoderConfig() zapcore.EncoderConfig {
	cfg := zap.NewDevelopmentEncoderConfig()
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	cfg.EncodeCaller = zapcore.ShortCallerEncoder
	return cfg
}
