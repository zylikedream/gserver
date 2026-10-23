package gxypgx

import (
	"context"
	"strings"
	"time"

	"gserver/core/gxymetrics"
	"gserver/core/gxytrace"

	"github.com/cockroachdb/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

type metricsPlugin struct{}

func (p *metricsPlugin) Name() string { return "gxymetrics" }

// Initialize 注册计时回调。
//
// 覆盖六类:Query/Create/Update/Delete 与 Raw/Row。后两类不能省——gorm 把
// Exec 与 Raw(...).Scan 分别交给 Raw 与 Row 处理器,不注册就完全不在指标里;
// 归属 fencing(Exec 的 upsert、Raw 的 SELECT ... FOR UPDATE)、序列号这类关键
// 路径恰好都是裸 SQL。
func (p *metricsPlugin) Initialize(db *gorm.DB) error {
	if err := db.Callback().Query().Before("gorm:query").Register("metrics:before_query", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Query().After("gorm:query").Register("metrics:after_query", afterQuery); err != nil {
		return err
	}
	if err := db.Callback().Create().Before("gorm:create").Register("metrics:before_create", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Create().After("gorm:create").Register("metrics:after_create", afterQuery); err != nil {
		return err
	}
	if err := db.Callback().Update().Before("gorm:update").Register("metrics:before_update", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Update().After("gorm:update").Register("metrics:after_update", afterQuery); err != nil {
		return err
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register("metrics:before_delete", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Delete().After("gorm:delete").Register("metrics:after_delete", afterQuery); err != nil {
		return err
	}
	if err := db.Callback().Raw().Before("gorm:raw").Register("metrics:before_raw", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Raw().After("gorm:raw").Register("metrics:after_raw", afterQuery); err != nil {
		return err
	}
	if err := db.Callback().Row().Before("gorm:row").Register("metrics:before_row", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Row().After("gorm:row").Register("metrics:after_row", afterQuery); err != nil {
		return err
	}
	return nil
}

func beforeQuery(db *gorm.DB) {
	db.InstanceSet("metrics:start_time", time.Now())

	// 一条查询一个 Client span;只在被采样的链路里产生(规则见 StartClientSpan)。
	// 执行沿用语句自己的 ctx(span 只负责覆盖时长),因此这里只要 span。
	_, span := gxytrace.StartClientSpan(db.Statement.Context, "postgres.query")
	if !span.IsRecording() {
		return
	}
	db.InstanceSet("metrics:span", span)
}

func afterQuery(db *gorm.DB) {
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if start, ok := db.InstanceGet("metrics:start_time"); ok {
		// 带 exemplar:DB 指标没有标签,exemplar 是唯一能把"这条慢查询"定位到具体
		// trace(进而看到 SQL 与调用方)的线索。ctx 里没有采样中的 span 时退化为普通观测。
		gxymetrics.ObserveWithTrace(ctx, gxymetrics.DBQueryDuration, time.Since(start.(time.Time)).Seconds())
	}

	raw, ok := db.InstanceGet("metrics:span")
	if !ok {
		return
	}
	span, ok := raw.(trace.Span)
	if !ok {
		return
	}
	span.SetAttributes(
		attribute.String("db.system", "postgresql"),
		attribute.String("db.operation", sqlOperation(db.Statement.SQL.String())),
		attribute.String("db.sql.table", db.Statement.Table),
	)
	if err := db.Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// sqlOperation 取 SQL 的第一个词(SELECT/INSERT/...),作为低基数属性。
// 不记录完整 statement:Raw SQL 可能拼了值,vars 也可能含敏感数据。
func sqlOperation(sql string) string {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return ""
	}
	if i := strings.IndexAny(sql, " \t\r\n"); i > 0 {
		sql = sql[:i]
	}
	return strings.ToUpper(sql)
}
