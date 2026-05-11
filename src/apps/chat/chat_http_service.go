package chat

import (
	"context"

	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	"gserver/core/gxyservice"
)

type chatHttpService struct {
	gxyhttp.HttpService
}

func NewChatHttpService() *chatHttpService {
	return &chatHttpService{}
}

func (s *chatHttpService) ServiceName() string {
	return "chat-http"
}

func (s *chatHttpService) OnModStart(ctx context.Context) error {
	host := gxyservice.ServiceApp().Host
	svr := gxyhttp.HttpSystem().NewHttpServer(host)
	gxyhttp.SetHandler(svr, ctx, s.ServiceName(), &ChatHandler{})
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
