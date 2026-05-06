package friend

import (
	"context"
	"gserver/core/gxyservice"
	"gserver/gameconfig"
	"gserver/core/gxyapp"
)

type friendApp struct {
	gxyapp.App
}

func NewFriendApp() *friendApp {
	return &friendApp{}
}

func (f *friendApp) ServiceName() string {
	return "friend"
}

func (f *friendApp) OnModInit(ctx context.Context) error {
	f.AddModule(ctx, gameconfig.NewGameConfig())
	gxyservice.ServiceApp().LoadService(ctx, NewFriendService())
	return nil
}
