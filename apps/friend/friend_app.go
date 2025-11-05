package friend

import (
	"context"

	"gserver/apps/friend/internal/logic"
	"gserver/core/gxyapp.go"
	"gserver/core/gxyhttp"
	"gserver/core/gxyservice"
)

const (
	SERVICE_NAME = "friend"
)

type friendApp struct {
	gxyapp.App
}

func NewFriendApp() *friendApp {
	app := &friendApp{}
	return app
}

func (f *friendApp) OnModInit(ctx context.Context) error {
	gxyservice.ServiceApp().LoadService(ctx, NewFriendService())
	return nil
}

type friendService struct {
	gxyhttp.HttpService
}

func NewFriendService() *friendService {
	return &friendService{}
}

func (f *friendService) ServiceName() string {
	return SERVICE_NAME
}

func (f *friendService) OnModInit(ctx context.Context) error {
	f.SetHandler(ctx, f.ServiceName(), logic.NewFriendServer())
	return nil
}
