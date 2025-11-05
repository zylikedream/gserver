package friend

import (
	"context"

	service "gserver/apps"
	"gserver/apps/friend/internal/logic"
	"gserver/core/gxyapp.go"
	"gserver/core/gxyhttp"
	"gserver/core/gxyservice"
)

const (
	SERVICE_NAME = service.FRIEND_SERVICE
)

type friendApp struct {
	gxyapp.App
	gxyhttp.HttpService
}

func NewFriendApp() *friendApp {
	app := &friendApp{}
	return app
}

func (f *friendApp) OnModInit(ctx context.Context) error {
	gxyservice.ServiceApp().LoadService(f)
	f.SetHandler(ctx, f.ServiceName(), logic.NewFriendServer())
	return nil
}

func (f *friendApp) ServiceName() string {
	return SERVICE_NAME
}
