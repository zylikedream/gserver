package chat

import (
	"context"

	"gserver/core/gxyactor"

	"ergo.services/ergo/act"
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
	if err := gxyactor.RegisterActorKind(s.ServiceName(), func() act.ActorBehavior {
		return NewChannelActor()
	}); err != nil {
		return err
	}
	return nil
}

func (s *chatService) OnModStop(ctx context.Context) error {
	gxyactor.DeregisterActorKind(s.ServiceName())
	return nil
}
