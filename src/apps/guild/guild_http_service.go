package guild

import (
	"context"

	"gserver/core/gxyhttp"
	"gserver/core/gxyservice"
	guildlogic "gserver/src/apps/guild/logic"

	"github.com/gogf/gf/v2/os/glog"
)

type guildHttpService struct {
	gxyhttp.HttpService
}

func NewGuildHttpService() *guildHttpService {
	return &guildHttpService{}
}

// ServiceName 使用 guild-http 避免与 guild actor 的 Consul 服务名冲突
func (s *guildHttpService) ServiceName() string {
	return "guild-http"
}

func (s *guildHttpService) OnModStart(ctx context.Context) error {
	host := gxyservice.ServiceApp().Host
	svr := gxyhttp.HttpSystem().NewHttpServer(host)
	gxyhttp.SetHandler(svr, ctx, s.ServiceName(), &guildlogic.GuildHandler{})
	glog.Infof(ctx, "guild server starting")
	if err := svr.Start(); err != nil {
		return err
	}
	s.Svr = svr
	return nil
}

func (s *guildHttpService) OnModStop(ctx context.Context) error {
	glog.Infof(ctx, "guild service stopping")
	return s.Svr.Shutdown()
}
