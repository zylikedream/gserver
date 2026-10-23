package gxypgx

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestQuerySpanIsChildOfStatementCtx 在已追踪的链路里,一次查询必须产生一个
// postgres.query 子 span(父 = ctx 里的 span),并带上低基数 db 属性。
func TestQuerySpanIsChildOfStatementCtx(t *testing.T) {
	db, mock := openMetricsTestDB(t)
	exp := withRecordingProvider(t)

	parent := trace.SpanID{0x11, 0x22}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{7},
		SpanID:     parent,
		TraceFlags: trace.FlagsSampled,
	}))

	mock.ExpectQuery(`SELECT 1`).WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(1))
	var x int
	if err := db.WithContext(ctx).Raw(`SELECT 1`).Scan(&x).Error; err != nil {
		t.Fatalf("raw scan: %v", err)
	}

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("span 数 = %d, want 1", len(spans))
	}
	if spans[0].Name != "postgres.query" {
		t.Fatalf("span 名 = %q, want postgres.query", spans[0].Name)
	}
	if spans[0].Parent.SpanID() != parent {
		t.Fatalf("父 span = %s, want %s", spans[0].Parent.SpanID(), parent)
	}
	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	if attrs["db.system"] != "postgresql" {
		t.Fatalf("db.system = %q, want postgresql", attrs["db.system"])
	}
	if attrs["db.operation"] != "SELECT" {
		t.Fatalf("db.operation = %q, want SELECT", attrs["db.operation"])
	}
}

// TestQueryOutsideTraceHasNoSpan 没有父链路时不新开 trace(后台任务、启动期查询)。
func TestQueryOutsideTraceHasNoSpan(t *testing.T) {
	db, mock := openMetricsTestDB(t)
	exp := withRecordingProvider(t)

	mock.ExpectQuery(`SELECT 1`).WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(1))
	var x int
	if err := db.Raw(`SELECT 1`).Scan(&x).Error; err != nil {
		t.Fatalf("raw scan: %v", err)
	}
	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("没有父链路时导出了 %d 个 span, want 0", n)
	}
}

func withRecordingProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return exp
}
