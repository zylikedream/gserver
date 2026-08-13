package chat

import (
	"context"
	"fmt"

	"gserver/core/gxyhttp"
	"gserver/core/gxylog"

	"github.com/gogf/gf/v2/frame/g"
)

type chatHttpService struct {
	gxyhttp.HttpService
	host string
}

func NewChatHttpService(host string) *chatHttpService {
	return &chatHttpService{
		host: host,
	}
}

func (s *chatHttpService) ServiceName() string {
	return "chat-http"
}

func (s *chatHttpService) OnModStart(ctx context.Context) error {
	port := g.Cfg().MustGet(ctx, "port.chat").Int()
	svr := gxyhttp.HttpSystem().NewHttpServer(fmt.Sprintf("%s:%d", s.host, port))
	gxyhttp.SetHandler(svr, ctx, s.ServiceName(), NewChatHandler())
	gxylog.Info(ctx, "chat http server starting")
	if err := svr.Start(); err != nil {
		return err
	}
	s.Svr = svr
	return nil
}

func (s *chatHttpService) OnModStop(ctx context.Context) error {
	gxylog.Info(ctx, "chat http service stopping")
	return s.Svr.Shutdown()
}
