package gxypgx

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxylog"

	"github.com/cockroachdb/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// gormLogger 把 gorm 的日志接到 gxylog。
//
// gorm 自带的 logger 直接写 stdout:没有结构化字段、没有 trace_id;而业务拿到
// error 后通常还会用 gxylog 再记一次,于是同一个错误两条日志、格式不同。
// 这里只保留 Error 级别的 SQL 诊断(SQL 文本、行数、耗时),其余级别交给业务日志。
type gormLogger struct{ level logger.LogLevel }

func (l gormLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.level = level
	return l
}

func (l gormLogger) Info(ctx context.Context, msg string, data ...any) {
	if l.level >= logger.Info {
		gxylog.Info(ctx, fmt.Sprintf(msg, data...))
	}
}

func (l gormLogger) Warn(ctx context.Context, msg string, data ...any) {
	if l.level >= logger.Warn {
		gxylog.Warn(ctx, fmt.Sprintf(msg, data...))
	}
}

func (l gormLogger) Error(ctx context.Context, msg string, data ...any) {
	if l.level >= logger.Error {
		gxylog.Error(ctx, fmt.Sprintf(msg, data...))
	}
}

// Trace 是 gorm 记录单条语句的地方:只在出错时记,不把每条 SQL 都写进日志。
func (l gormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level < logger.Error {
		return
	}
	// ErrRecordNotFound 是查询语义的一部分(没查到),不是故障;与 gorm 默认
	// logger 的行为一致。
	if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		return
	}
	sql, rows := fc()
	gxylog.Error(ctx, "gorm query failed",
		gxylog.Str("sql", sql),
		gxylog.Num("rows", rows),
		gxylog.Str("elapsed", time.Since(begin).String()),
		gxylog.Err(err),
	)
}
