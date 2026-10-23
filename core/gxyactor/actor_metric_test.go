package gxyactor

import (
	"testing"

	"gserver/core/gxymetrics"

	"ergo.services/ergo/gen"
	dto "github.com/prometheus/client_model/go"
)

// TestActorMessageDurationRecorded 门面必须记录业务入口的耗时。
// 为什么需要:指标定义过却从未记录,面板永远是空的——actor 处理耗时是性能优化的
// 第一手数据,而且带 exemplar 后能从慢样本直接跳到对应 trace。
func TestActorMessageDurationRecorded(t *testing.T) {
	_, subj := spawnPlain(t) // kind = "plain"

	before := actorDurationSamples(t, "plain")
	subj.SendMessage(gen.PID{}, "hello")
	if after := actorDurationSamples(t, "plain"); after != before+1 {
		t.Fatalf("actor_message_duration_seconds 样本数 = %d, want %d", after, before+1)
	}
}

func actorDurationSamples(t *testing.T, kind string) uint64 {
	t.Helper()
	observer := gxymetrics.ActorMessageDuration.WithLabelValues(kind)
	metric, ok := observer.(interface{ Write(*dto.Metric) error })
	if !ok {
		t.Fatalf("actor message histogram does not implement prometheus.Metric")
	}
	var value dto.Metric
	if err := metric.Write(&value); err != nil {
		t.Fatalf("write actor message histogram: %v", err)
	}
	return value.GetHistogram().GetSampleCount()
}
