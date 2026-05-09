package chat

import (
	"context"

	"gserver/core/gxyactor"
	"gserver/src/lib"
)

type chatService struct {
	gxyactor.ActorService
}

func NewChatService() *chatService {
	return &chatService{}
}

func (s *chatService) ServiceName() string {
	return lib.CHANNEL_ACTOR_TYPE
}

func (s *chatService) OnModStart(ctx context.Context) error {
	// 注册 ChannelActor kind（consistent hash 按 channel_type:channel_id 路由）
	gxyactor.RegisterActorKind(s.ServiceName(), func() gxyactor.IActor {
		return NewChannelActor()
	})
	return nil
}

func (s *chatService) OnModStop(ctx context.Context) error {
	gxyactor.DeregisterActorKind(s.ServiceName())
	return nil
}
