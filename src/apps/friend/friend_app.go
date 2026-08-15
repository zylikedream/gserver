package friend

import (
	"context"
	"gserver/core/gxyapp"
	"gserver/core/gxypgx"
	"gserver/core/gxyservice"
	fl "gserver/src/apps/friend/logic"
	"gserver/src/pkg/gameconfig"
)

type friendApp struct {
	gxyapp.App
	host string
}

func NewFriendApp(host string) *friendApp {
	return &friendApp{
		host: host,
	}
}

func (f *friendApp) ServiceName() string {
	return "friend"
}

func (f *friendApp) OnModInit(ctx context.Context) error {
	if err := f.AddModule(ctx, gameconfig.NewGameConfig()); err != nil {
		return err
	}
	fl.InitFriendSchema(ctx, gxypgx.DB())
	gxyservice.ServiceApp().LoadService(ctx, NewFriendService(f.host))
	return nil
}
