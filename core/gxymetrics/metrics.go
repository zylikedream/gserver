package gxymetrics

import (
	"context"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

type metricsConfig struct {
	Addr    string `json:"addr"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

type metricsApp struct {
	gxyapp.App
	conf *metricsConfig

	// runtimeRegistry 是 actor 运行时自有指标的注册表,由运行时侧在启动后登记。
	// 采集时与项目指标一起并入同一端点(见 ADR 0013)。
	runtimeRegistry prometheus.Gatherer
}

// SetRuntimeRegistry 登记运行时的指标注册表。
// 由 actor 运行时在启动后调用;早于本调用发生的采集不含运行时指标。
func SetRuntimeRegistry(g prometheus.Gatherer) {
	if app == nil {
		return
	}
	app.runtimeRegistry = g
}

// gatherer 返回对外暴露的采集器。
//
// 必须每次采集时动态解析:运行时的注册表晚于本模块启动才登记(见 gxyactor),
// 若在此处一次性绑定,端点会永远停在"未接入"的形态。
func (m *metricsApp) gatherer() prometheus.Gatherer {
	return dynamicGatherer{extra: func() prometheus.Gatherer { return m.runtimeRegistry }}
}

// dynamicGatherer 把运行时的注册表并入项目指标;未登记时只用项目指标。
type dynamicGatherer struct {
	extra func() prometheus.Gatherer
}

func (d dynamicGatherer) Gather() ([]*dto.MetricFamily, error) {
	outer := d.extra()
	if outer == nil {
		return prometheus.DefaultGatherer.Gather()
	}
	return prometheus.Gatherers{prometheus.DefaultGatherer, outer}.Gather()
}

var app *metricsApp

func NewMetricsApp() *metricsApp {
	app = &metricsApp{
		conf: &metricsConfig{
			Addr:    ":9090",
			Path:    "/metrics",
			Enabled: true,
		},
	}
	return app
}

func (m *metricsApp) OnModInit(ctx context.Context) error {
	if err := gxyutil.CfgUnmarshalKey(ctx, g.Cfg(), "metrics", m.conf); err != nil {
		return err
	}
	if !m.conf.Enabled {
		return nil
	}
	prometheus.MustRegister(
		TcpConnections,
		ActorActiveCount,
		ActorMessages,
		ActorMessageDuration,
		DBQueryDuration,
		RedisRequestDuration,
		OnlinePlayers,
		ClientRequests,
		ClientRequestDuration,
		RoleModuleLimitTotal,
		RoleModuleDisabled,
		GatewayPackets,
		SessionDisconnects,
		LoginInflight,
		LoginQueueLength,
		LoginLimitTotal,
		LoginWaitDuration,
		RoleLogins,
		RoleLogouts,
		RoleNotifyPublish,
		RoleNotifyConsume,
		ActorLocate,
	)
	runtime.SetBlockProfileRate(10000000) // 只采样 ≥10ms 的同步阻塞
	runtime.SetMutexProfileFraction(100)  // 采样 1% 的锁竞争
	return nil
}

func (m *metricsApp) OnModStart(ctx context.Context) error {
	if !m.conf.Enabled {
		return nil
	}
	http.Handle(m.conf.Path, promhttp.HandlerFor(m.gatherer(), promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))
	go func() {
		// 端口冲突等瞬时错误:重试 5 次(间隔 2s),仍失败则降级(metrics 不可用,不影响游戏进程)
		for i := range 5 {
			if err := http.ListenAndServe(m.conf.Addr, nil); err != nil {
				gxylog.Error(ctx, "metrics server error, retrying", gxylog.Err(err), gxylog.Num("retry", int64(i+1)))
				time.Sleep(2 * time.Second)
				continue
			}
			return
		}
		gxylog.Warn(ctx, "metrics server failed after retries, metrics disabled", gxylog.Str("addr", m.conf.Addr))
	}()
	gxylog.Info(ctx, "metrics server started", gxylog.Str("addr", m.conf.Addr), gxylog.Str("path", m.conf.Path))
	return nil
}
