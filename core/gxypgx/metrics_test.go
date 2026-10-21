package gxypgx

import (
	"context"
	"slices"
	"testing"

	"gserver/core/gxymetrics"
	"gserver/core/gxytrace"

	"ergo.services/ergo/gen"
	"github.com/DATA-DOG/go-sqlmock"
	dto "github.com/prometheus/client_model/go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openMetricsTestDB 搭一个挂了指标插件、不探活的 gorm。
func openMetricsTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger:               gormLogger{level: logger.Silent},
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("gorm open: %v", err)
	}
	if err := db.Use(&metricsPlugin{}); err != nil {
		t.Fatalf("use plugin: %v", err)
	}
	return db, mock
}

// TestMetricsPluginCoversBareSQL 裸 SQL 必须计入 db_query_duration_seconds。
// 为什么需要:gorm 把 Exec 交给 Raw 处理器、Raw(...).Scan 交给 Row 处理器,
// 与 Query/Create/Update/Delete 是不同回调;不注册时,归属 fencing(fence 的
// upsert 与 SELECT ... FOR UPDATE)完全不在指标里 —— 关键路径恰好是裸 SQL,
// 看板却是空的。
func TestMetricsPluginCoversBareSQL(t *testing.T) {
	db, mock := openMetricsTestDB(t)
	before := dbQuerySampleCount(t)

	// Exec → Raw 回调
	mock.ExpectExec(`UPDATE t SET x = 1`).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := db.Exec(`UPDATE t SET x = 1`).Error; err != nil {
		t.Fatalf("exec: %v", err)
	}

	// Raw(...).Scan → Row 回调
	mock.ExpectQuery(`SELECT 1`).WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(1))
	var x int
	if err := db.Raw(`SELECT 1`).Scan(&x).Error; err != nil {
		t.Fatalf("raw scan: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if after := dbQuerySampleCount(t); after != before+2 {
		t.Fatalf("db_query_duration_seconds 样本数 = %d, want %d(裸 SQL 未计入)", after, before+2)
	}
}

// TestMetricsPluginAttachesExemplar DB 指标没有标签:慢样本必须带 trace_id,
// 否则无法定位到具体查询与调用方。
func TestMetricsPluginAttachesExemplar(t *testing.T) {
	db, mock := openMetricsTestDB(t)

	ctx := gxytrace.ContextWithErgoTrace(context.Background(),
		gen.Tracing{ID: [2]uint64{0x1111, 0x2222}, SpanID: 3})
	want := gxymetrics.TraceIDFromContext(ctx)
	if want == "" {
		t.Fatal("测试上下文必须带 trace_id")
	}

	mock.ExpectQuery(`SELECT 1`).WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(1))
	var x int
	if err := db.WithContext(ctx).Raw(`SELECT 1`).Scan(&x).Error; err != nil {
		t.Fatalf("raw scan: %v", err)
	}

	if ids := exemplarTraceIDs(t, gxymetrics.DBQueryDuration); !slices.Contains(ids, want) {
		t.Fatalf("样本未带 exemplar trace_id=%s(实际 %v)", want, ids)
	}
}

func dbQuerySampleCount(t *testing.T) uint64 {
	t.Helper()
	var value dto.Metric
	if err := gxymetrics.DBQueryDuration.Write(&value); err != nil {
		t.Fatalf("write db query histogram: %v", err)
	}
	return value.GetHistogram().GetSampleCount()
}

func exemplarTraceIDs(t *testing.T, observer interface {
	Write(*dto.Metric) error
}) []string {
	t.Helper()
	var value dto.Metric
	if err := observer.Write(&value); err != nil {
		t.Fatalf("write histogram: %v", err)
	}
	var ids []string
	for _, bucket := range value.GetHistogram().GetBucket() {
		exemplar := bucket.GetExemplar()
		if exemplar == nil {
			continue
		}
		for _, label := range exemplar.GetLabel() {
			if label.GetName() == "trace_id" {
				ids = append(ids, label.GetValue())
			}
		}
	}
	return ids
}
