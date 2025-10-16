package gateway

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxynet"
	"gserver/core/gxynet/endpoint"
	"gserver/service"
	"gserver/service/gateway/internal/logic"

	"github.com/asynkron/protoactor-go/actor"
)

// sessionSupervisor 会话管理器 - 直接继承gen.Supervisor，本身即是Supervisor
type gateService struct {
	gxyactor.InnerService
	network *gxynet.Network
}

var gate = newGateService()

func newGateService() *gateService {
	return &gateService{}
}

func GateService() *gateService {
	return gate
}

func (s *gateService) Name() string {
	return service.GATE_SERVICE
}

func (s *gateService) OnInit(ctx context.Context) error {
	s.network = gxynet.NewNetwork("config/gate.net.toml", NewGateHandler())
	return nil
}

func (s *gateService) OnStart(ctx context.Context) error {
	if err := s.network.Start(ctx); err != nil {
		return err
	}
	// 启动会话管理器
	return nil
}

func (s *gateService) SpawnSession(ep endpoint.Endpoint) (gxyactor.PID, error) {
	return gxyactor.ActorSystem().Spawn(func() actor.Actor {
		return logic.NewSession(ep)
	})
}

func (s *gateService) StopSession(pid gxyactor.PID) error {
	return gxyactor.ActorSystem().StopActor(pid)
}
