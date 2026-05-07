package chat

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/gameconfig"
)

type chatApp struct {
	gxyapp.App
}

func NewChatApp() *chatApp {
	return &chatApp{}
}

func (c *chatApp) ServiceName() string {
	return "chat"
}

func (c *chatApp) OnModInit(ctx context.Context) error {
	c.AddModule(ctx, gameconfig.NewGameConfig())
	InitChatSchema(ctx)
	return nil
}

func (c *chatApp) OnModStart(ctx context.Context) error {
	StartRelay(ctx)
	return nil
}

func (c *chatApp) OnModStop(ctx context.Context) error {
	StopRelay()
	return nil
}
