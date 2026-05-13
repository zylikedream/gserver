package guild

import (
	"context"
	"fmt"

	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	guildlogic "gserver/src/apps/guild/logic"

	"github.com/gogf/gf/v2/frame/g"
)

type guildHttpService struct {
	gxyhttp.HttpService
	host string
}

func NewGuildHttpService(host string) *guildHttpService {
	return &guildHttpService{
		host: host,
	}
}

// ServiceName 使用 guild-http 避免与 guild actor 的 Consul 服务名冲突
func (s *guildHttpService) ServiceName() string {
	return "guild-http"
}

func (s *guildHttpService) OnModStart(ctx context.Context) error {
	port := g.Cfg().MustGet(ctx, "port.guild").Int()
	svr := gxyhttp.HttpSystem().NewHttpServer(fmt.Sprintf("%s:%d", s.host, port))
	gxyhttp.SetHandler(svr, ctx, s.ServiceName(), &guildlogic.GuildHandler{})
	gxylog.Info(ctx, "guild server starting")
	if err := svr.Start(); err != nil {
		return err
	}
	s.Svr = svr
	return nil
}

func (s *guildHttpService) OnModStop(ctx context.Context) error {
	gxylog.Info(ctx, "guild service stopping")
	return s.Svr.Shutdown()
}
