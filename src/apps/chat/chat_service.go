package chat

import (
	"context"

	"gserver/core/gxyhttp"
	"gserver/core/gxyservice"

	"github.com/gogf/gf/v2/os/glog"
)

type chatService struct {
	gxyhttp.HttpService
}

func NewChatService() *chatService {
	return &chatService{}
}

func (s *chatService) ServiceName() string {
	return "chat"
}

func (s *chatService) OnModStart(ctx context.Context) error {
	host := gxyservice.ServiceApp().Host
	svr := gxyhttp.HttpSystem().NewHttpServer(host)
	gxyhttp.SetHandler(svr, ctx, "chat", &ChatHandler{})
	glog.Infof(ctx, "chat server starting")
	if err := svr.Start(); err != nil {
		return err
	}
	s.Svr = svr
	return nil
}

func (s *chatService) OnModStop(ctx context.Context) error {
	glog.Infof(ctx, "chat service stopping")
	return s.Svr.Shutdown()
}
