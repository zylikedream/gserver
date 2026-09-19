package gxytrace

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/frame/g"
)

type traceConfig struct {
	Endpoint string `json:"endpoint"`
	Enabled  bool   `json:"enabled"`
	// SampleRate 是运行时追踪的采样率:链路是否被追踪按此比例决定。
	// 未被采样的链路不产生任何 span,因此这是控制追踪开销的主要手段。
	SampleRate float64 `json:"sample_rate" toml:"sample_rate"`
}

type traceApp struct {
	gxyapp.App
	conf     *traceConfig
	shutdown func()
}

var app *traceApp

func NewTraceApp() *traceApp {
	app = &traceApp{
		conf: &traceConfig{
			Endpoint: "localhost:4317",
			Enabled:  true,
			// 默认只追踪少量链路:每次消息投递会产生多个观测,
			// 全量追踪的写入量远大于业务本身的量级。
			SampleRate: 0.01,
		},
	}
	return app
}

func (t *traceApp) OnModInit(ctx context.Context) error {
	if err := gxyutil.CfgUnmarshalKey(ctx, g.Cfg(), "trace", t.conf); err != nil {
		return err
	}
	if !t.conf.Enabled {
		return nil
	}
	shutdown, err := InitTracerProvider(ctx, g.Cfg().MustGet(ctx, "node.name").String(), t.conf.Endpoint)
	if err != nil {
		return err
	}
	t.shutdown = shutdown
	return nil
}

func (t *traceApp) OnModStart(ctx context.Context) error {
	if !t.conf.Enabled {
		return nil
	}
	gxylog.Info(ctx, "trace provider initialized",
		gxylog.Str("endpoint", t.conf.Endpoint),
		gxylog.Any("ergo_sample_rate", t.conf.SampleRate))
	return nil
}

func (t *traceApp) OnModStop(ctx context.Context) error {
	if t.shutdown != nil {
		t.shutdown()
	}
	return nil
}

// Enabled 报告追踪是否启用。未启用时全局 provider 为空实现,
// 生成的 span 会被丢弃,因此调用方无需再判断。
func (t *traceApp) Enabled() bool {
	return t != nil && t.conf.Enabled
}

// SampleRate 返回运行时追踪的采样率。
func (t *traceApp) SampleRate() float64 {
	if t == nil {
		return 0
	}
	return t.conf.SampleRate
}

// Enabled 报告追踪是否启用。
func Enabled() bool { return app.Enabled() }

// SampleRate 返回运行时追踪的采样率。
func SampleRate() float64 { return app.SampleRate() }
