package friend

import (
	"context"
	"fmt"
	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	fl "gserver/src/apps/friend/logic"

	"github.com/gogf/gf/v2/frame/g"
)

type friendService struct {
	gxyhttp.HttpService
	host string
}

func NewFriendService(host string) *friendService {
	return &friendService{
		host: host,
	}
}

func (s *friendService) ServiceName() string {
	return "friend"
}

func (s *friendService) OnModStart(ctx context.Context) error {
	port := g.Cfg().MustGet(ctx, "port.friend").Int()
	svr := gxyhttp.HttpSystem().NewHttpServer(fmt.Sprintf("%s:%d", s.host, port))
	gxyhttp.SetHandler(svr, ctx, s.ServiceName(), &fl.FriendHandler{})
	gxylog.Info(ctx, "friend server starting")
	if err := svr.Start(); err != nil {
		return err
	}
	s.Svr = svr
	return nil
}

func (s *friendService) OnModStop(ctx context.Context) error {
	gxylog.Info(ctx, "friend service stopping")
	return s.Svr.Shutdown()
}
