package guild

import (
	"context"

	"gserver/core/gxyactor"
	guildlogic "gserver/src/apps/guild/logic"
)

type guildService struct {
	gxyactor.ActorService
}

func NewGuildService() *guildService {
	return &guildService{}
}

func (s *guildService) ServiceName() string {
	return "guild"
}

func (s *guildService) OnModStart(ctx context.Context) error {
	gxyactor.RegisterActorKind(s.ServiceName(), func() gxyactor.IActor {
		return guildlogic.NewGuildActor()
	})
	return nil
}

func (s *guildService) OnModStop(ctx context.Context) error {
	gxyactor.DeregisterActorKind(s.ServiceName())
	return nil
}
