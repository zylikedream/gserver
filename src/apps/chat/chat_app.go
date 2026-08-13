package chat

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxypgx"
	"gserver/core/gxyservice"
	"gserver/src/pkg/gameconfig"
)

type chatApp struct {
	gxyapp.App
	host string
}

func NewChatApp(host string) *chatApp {
	return &chatApp{
		host: host,
	}
}

func (c *chatApp) OnModInit(ctx context.Context) error {
	c.AddModule(ctx, gameconfig.NewGameConfig())
	InitChatSchema(ctx, gxypgx.DB())
	gxyservice.ServiceApp().LoadService(ctx, NewChatService())
	gxyservice.ServiceApp().LoadService(ctx, NewChatHttpService(c.host))

	return nil
}
