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
		gxylog.Str("endpoint", t.conf.Endpoint))
	return nil
}

func (t *traceApp) OnModStop(ctx context.Context) error {
	if t.shutdown != nil {
		t.shutdown()
	}
	return nil
}
