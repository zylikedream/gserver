package gxyapp

import (
	"gserver/core/gxymodule"

	"github.com/gogf/gf/v2/text/gstr"
)

var apps map[string]IApp

func RegisterApp(app IApp) {
	if apps == nil {
		apps = make(map[string]IApp)
	}
	apps[app.AppName()] = app
}

func GetApp(appName string) IApp {
	return apps[appName]
}

type IApp interface {
	gxymodule.IModule
	Deps() []IApp
	AppName() string
}

type App struct {
	gxymodule.ModuleBase
}

func (a *App) Deps() []IApp {
	return nil
}

func (a *App) AppName() string {
	name := a.GetModName()
	// 去掉_app
	name = gstr.TrimRightStr(name, "_app")
	return name
}
