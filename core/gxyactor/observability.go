package gxyactor

import (
	"context"
	"net/http"

	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxytrace"

	"ergo.services/actor/metrics"
	"ergo.services/ergo/gen"
)

// 本文件把运行时的可观测性接入项目既有的出口(ADR 0013):
// 追踪转成 OTel span 送入既有导出器;指标并入既有的单一指标端点。
// 两者都在节点进入 Running 之后挂载——运行时只在该状态下接受导出器与注册表登记。

// installTracing 把运行时的追踪观测接入既有追踪导出器。
func (a *actorApp) installTracing(ctx context.Context) error {
	if a.node == nil || !gxytrace.Enabled() {
		return nil
	}
	// 只追踪消息收发:进程创建/终止的观测量随激活次数增长,与业务链路无关。
	flags := gen.TracingFlagSend | gen.TracingFlagReceive
	if err := a.node.TracingExporterAdd("otel", gxytrace.NewErgoExporter(), flags); err != nil {
		return err
	}
	// 采样率决定"多少条链路被追踪"。未被采样的链路不产生任何观测,
	// 因此这是控制追踪开销的主要手段(见 gxytrace 的 trace.sample_rate)。
	if err := a.node.SetTracingSampler(gen.TracingSamplerRatio(gxytrace.SampleRate())); err != nil {
		return err
	}
	gxylog.Info(ctx, "runtime tracing exporter installed",
		gxylog.Str("flags", flags.String()),
		gxylog.Any("sample_rate", gxytrace.SampleRate()))
	return nil
}

// startRuntimeMetrics 采集运行时指标,并并入既有的单一指标端点。
func (a *actorApp) startRuntimeMetrics(ctx context.Context) error {
	if a.node == nil {
		return nil
	}
	// 必须先登记消息类型,否则指标采集无法解码,数值会静默保持为零。
	if err := a.node.Network().RegisterTypes(metrics.NetworkTypes()); err != nil {
		return err
	}
	if err := a.node.Network().RegisterErrors(metrics.ErrorTypes()); err != nil {
		return err
	}

	shared := metrics.NewShared()
	// Mux 用于让采集器不自建端点:给的 mux 不对外服务,聚合端点仍由 gxymetrics
	// 提供。运行时的基础指标只在"共享 + 端点"这一角色下才会采集,而该角色要求
	// 指定 Mux 或端口——故以不服务的 mux 满足它,避免多出第二个端点。
	_, err := a.node.Spawn(metrics.Factory, gen.ProcessOptions{}, metrics.Options{
		Shared: shared,
		Mux:    http.NewServeMux(),
	})
	if err != nil {
		return err
	}
	gxymetrics.SetRuntimeRegistry(shared.Registry())
	gxylog.Info(ctx, "runtime metrics collector started")
	return nil
}
