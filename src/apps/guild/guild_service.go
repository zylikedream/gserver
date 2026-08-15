package guild

import (
	"context"

	"gserver/core/gxyactor"
	guildlogic "gserver/src/apps/guild/logic"
	"gserver/src/lib"
)

type guildService struct {
	gxyactor.ActorService
}

func NewGuildService() *guildService {
	return &guildService{}
}

func (s *guildService) ServiceName() string {
	return lib.GUILD_ACTOR_TYPE
}

func (s *guildService) OnModStart(ctx context.Context) error {
	if err := gxyactor.RegisterActorKind(s.ServiceName(), func() gxyactor.IActor {
		return guildlogic.NewGuildActor()
	}); err != nil {
		return err
	}
	return nil
}

func (s *guildService) OnModStop(ctx context.Context) error {
	gxyactor.DeregisterActorKind(s.ServiceName())
	return nil
}
