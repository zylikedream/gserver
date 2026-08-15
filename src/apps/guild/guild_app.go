package guild

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxypgx"
	"gserver/core/gxyservice"
	guildlogic "gserver/src/apps/guild/logic"
	"gserver/src/pkg/gameconfig"
)

type guildApp struct {
	gxyapp.App
	host string
}

func NewGuildApp(host string) *guildApp {
	return &guildApp{
		host: host,
	}
}

func (g *guildApp) OnModInit(ctx context.Context) error {
	if err := g.AddModule(ctx, gameconfig.NewGameConfig()); err != nil {
		return err
	}
	guildlogic.InitGuildSchema(ctx, gxypgx.DB())
	gxyservice.ServiceApp().LoadService(ctx, NewGuildService())
	gxyservice.ServiceApp().LoadService(ctx, NewGuildHttpService(g.host))
	return nil
}
