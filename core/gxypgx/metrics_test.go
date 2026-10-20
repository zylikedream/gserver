package gxypgx

import (
	"testing"

	"gserver/core/gxymetrics"

	"github.com/DATA-DOG/go-sqlmock"
	dto "github.com/prometheus/client_model/go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestMetricsPluginCoversBareSQL 裸 SQL 必须计入 db_query_duration_seconds。
// 为什么需要:gorm 把 Exec 交给 Raw 处理器、Raw(...).Scan 交给 Row 处理器,
// 与 Query/Create/Update/Delete 是不同回调;不注册时,归属 fencing(fence 的
// upsert 与 SELECT ... FOR UPDATE)完全不在指标里 —— 关键路径恰好是裸 SQL,
// 看板却是空的。
func TestMetricsPluginCoversBareSQL(t *testing.T) {
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

func dbQuerySampleCount(t *testing.T) uint64 {
	t.Helper()
	metric, ok := any(gxymetrics.DBQueryDuration).(interface {
		Write(*dto.Metric) error
	})
	if !ok {
		t.Fatalf("db query histogram does not implement prometheus.Metric")
	}
	var value dto.Metric
	if err := metric.Write(&value); err != nil {
		t.Fatalf("write db query histogram: %v", err)
	}
	return value.GetHistogram().GetSampleCount()
}
