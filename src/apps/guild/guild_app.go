package guild

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxyservice"
	"gserver/src/pkg/gameconfig"
	guildlogic "gserver/src/apps/guild/logic"
)

type guildApp struct {
	gxyapp.App
}

func NewGuildApp() *guildApp {
	return &guildApp{}
}

func (g *guildApp) OnModInit(ctx context.Context) error {
	g.AddModule(ctx, gameconfig.NewGameConfig())
	guildlogic.InitGuildSchema(ctx)
	gxyservice.ServiceApp().LoadService(ctx, NewGuildService())
	gxyservice.ServiceApp().LoadService(ctx, NewGuildHttpService())
	return nil
}
