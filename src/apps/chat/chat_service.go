package chat

import (
	"context"

	"gserver/core/gxyactor"
	"gserver/core/gxyhttp"
	"gserver/core/gxyservice"
	"gserver/src/lib"

	"github.com/gogf/gf/v2/os/glog"
)

type chatService struct {
	gxyhttp.HttpService
}

func NewChatService() *chatService {
	return &chatService{}
}

func (s *chatService) ServiceName() string {
	return lib.CHANNEL_ACTOR_TYPE
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

	// 注册 ChannelActor kind（consistent hash 按 channel_type:channel_id 路由）
	gxyactor.RegisterActorKind(s.ServiceName(), func() gxyactor.IActor {
		return NewChannelActor()
	})

	return nil
}

func (s *chatService) OnModStop(ctx context.Context) error {
	gxyactor.DeregisterActorKind(s.ServiceName())
	glog.Infof(ctx, "chat service stopping")
	return s.Svr.Shutdown()
}
