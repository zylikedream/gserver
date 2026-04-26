package gxyapp

import (
	"gserver/core/gxymodule"
)

var apps map[string]IApp

func RegisterApp(name string, app IApp) {
	if apps == nil {
		apps = make(map[string]IApp)
	}
	app.SetAppName(name)
	apps[name] = app
}

func GetApp(appName string) IApp {
	return apps[appName]
}

type IApp interface {
	gxymodule.IModule
	AppName() string
	SetAppName(name string)
}

type App struct {
	gxymodule.ModuleBase
	appName string
	deps    []string
}

func (a *App) Deps() []string {
	return a.deps
}

func (a *App) SetDeps(deps []string) {
	a.deps = deps
}

func (a *App) AppName() string {
	return a.appName
}

func (a *App) SetAppName(name string) {
	a.appName = name
}
