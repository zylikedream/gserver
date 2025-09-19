package gxyapp

import (
	"context"
	"fmt"

	"gserver/core/gxymodule"

	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/glog"
)

type Application struct {
	rootModule gxymodule.Module
	AppID      string `toml:"app_id"`
	AppType    string `toml:"app_type"`
}

var app *Application

func App() *Application {
	return app
}

func InitApp(config string) *Application {
	app = &Application{}
	cfg := gcfg.Instance(config)
	ctx := context.Background()
	app.AppID = cfg.MustGet(ctx, "app.app_id").String()
	app.AppType = cfg.MustGet(ctx, "app.app_type").String()
	return app
}

func (a *Application) Start(ctx context.Context) error {
	for _, mod := range a.rootModule.Modules() {
		if err := mod.BaseModule().Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (a *Application) Stop(ctx context.Context) error {
	for _, mod := range a.rootModule.Modules() {
		if err := mod.BaseModule().Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (a *Application) Node() string {
	if a == nil {
		return "default.0"
	}
	return fmt.Sprintf("%s.%s", a.AppType, a.AppID)
}

func (a *Application) LoadModule(mod gxymodule.IModule) {
	if err := a.rootModule.AddModule(context.Background(), mod); err != nil {
		glog.Fatalf(context.Background(), "add module %v err: %s", mod.GetName(), err)
	}
}
