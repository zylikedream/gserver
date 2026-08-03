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
)

type metricsConfig struct {
	Addr    string `json:"addr"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

type metricsApp struct {
	gxyapp.App
	conf *metricsConfig
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
		GatewayPackets,
		SessionDisconnects,
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
	http.Handle(m.conf.Path, promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
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
