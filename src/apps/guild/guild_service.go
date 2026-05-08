package guild

import (
	"context"

	"gserver/core/gxyhttp"
	"gserver/core/gxyservice"
	guildlogic "gserver/src/apps/guild/logic"

	"github.com/gogf/gf/v2/os/glog"
)

type guildService struct {
	gxyhttp.HttpService
}

func NewGuildService() *guildService {
	return &guildService{}
}

func (s *guildService) ServiceName() string {
	return "guild"
}

func (s *guildService) OnModStart(ctx context.Context) error {
	host := gxyservice.ServiceApp().Host
	svr := gxyhttp.HttpSystem().NewHttpServer(host)
	gxyhttp.SetHandler(svr, ctx, "guild", &guildlogic.GuildHandler{})
	glog.Infof(ctx, "guild server starting")
	if err := svr.Start(); err != nil {
		return err
	}
	s.Svr = svr
	return nil
}

func (s *guildService) OnModStop(ctx context.Context) error {
	glog.Infof(ctx, "guild service stopping")
	return s.Svr.Shutdown()
}
