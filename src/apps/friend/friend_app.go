package friend

import (
	"context"
	"gserver/core/gxyapp"
	"gserver/core/gxyservice"
	"gserver/gameconfig"
	fl "gserver/src/apps/friend/logic"
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
	fl.InitFriendSchema(ctx)
	gxyservice.ServiceApp().LoadService(ctx, NewFriendService())
	return nil
}
