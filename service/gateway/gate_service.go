package gateway

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxymodule"
	"gserver/core/gxynet"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxyservice"
	"gserver/service"
	"gserver/service/gateway/internal/logic"

	"ergo.services/ergo/gen"
)

// sessionSupervisor 会话管理器 - 直接继承gen.Supervisor，本身即是Supervisor
type gateService struct {
	gxymodule.Module
	network *gxynet.Network
}

var gate *gateService

func NewGateService() *gateService {
	gate = &gateService{
		network: gxynet.NewNetwork("config/gate.net.toml", NewGateHandler()),
	}
	return gate
}

func GateService() *gateService {
	return gate
}

func (s *gateService) Name() string {
	return service.GATE_SERVICE
}

func (s *gateService) IsRemote() bool {
	return false
}

func (s *gateService) OnStart(ctx context.Context) error {
	return s.network.Start(ctx)
}

func (s *gateService) Worker() gxyservice.WorkerCreator {
	return func() gen.ProcessBehavior {
		return logic.NewSession()
	}
}

func (s *gateService) SpawnSession(ep endpoint.Endpoint) (gen.PID, error) {
	return gxyactor.ActorSystem().Spawn(gen.ProcessFactory(s.Worker()), gen.ProcessOptions{}, ep)
}

func (s *gateService) StopSession(pid gen.PID) error {
	return gxyactor.ActorSystem().StopActor(pid)
}
