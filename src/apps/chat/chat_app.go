package chat

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxyservice"
	"gserver/gameconfig"
)

type chatApp struct {
	gxyapp.App
}

func NewChatApp() *chatApp {
	return &chatApp{}
}

func (c *chatApp) OnModInit(ctx context.Context) error {
	c.AddModule(ctx, gameconfig.NewGameConfig())
	InitChatSchema(ctx)
	gxyservice.ServiceApp().LoadService(ctx, NewChatService())
	gxyservice.ServiceApp().LoadService(ctx, NewChatHttpService())

	return nil
}
