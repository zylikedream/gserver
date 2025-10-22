package gateway

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxynet"
	"gserver/core/gxynet/endpoint"
	"gserver/protocol/pb"
	"gserver/service"
	"gserver/service/gateway/internal/logic"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/errors/gerror"
)

// sessionSupervisor 会话管理器 - 直接继承gen.Supervisor，本身即是Supervisor
type gateService struct {
	gxyactor.InnerService
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

func (s *gateService) OnModInit(ctx context.Context) error {
	network := gxynet.NewNetwork("config/gate.net.toml", NewGateHandler())
	s.AddModule(ctx, network)
	logic.NewSessionMgr()
	return nil
}

func (s *gateService) OnModStart(ctx context.Context) error {
	// 启动会话管理器
	return nil
}

func (s *gateService) OnModStop(ctx context.Context) error {
	sessions := logic.SessionMgr().All()
	for _, pid := range sessions {
		s.StopSession(pid, gerror.New("gateway service stop"))
	}
	return nil
}

func (s *gateService) SpawnSession(ep endpoint.Endpoint) (gxyactor.PID, error) {
	return gxyactor.ActorSystem().Spawn(func() actor.Actor {
		return logic.NewSession(ep)
	})
}

func (s *gateService) StopSession(pid gxyactor.PID, err error) error {
	return gxyactor.ActorSystem().Send(pid, &pb.ActorStop{
		Reason: err.Error(),
	})
}
