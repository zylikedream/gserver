package gateway

import (
	"context"
	"gserver/apps/gateway/internal/logic"
	"gserver/core/gxyactor"
	"gserver/core/gxyapp.go"
	"gserver/core/gxynet"
	"gserver/core/gxynet/endpoint"
	"gserver/protocol/pb"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

// sessionSupervisor 会话管理器 - 直接继承gen.Supervisor，本身即是Supervisor
type gateApp struct {
	gxyapp.App
}

func NewGateApp() *gateApp {
	return &gateApp{}
}

func (s *gateApp) OnModInit(ctx context.Context) error {
	network := gxynet.NewNetwork(g.Cfg(), NewGateHandler())
	s.AddModule(ctx, network)
	logic.NewSessionMgr()
	return nil
}

func (s *gateApp) OnModStart(ctx context.Context) error {
	// 启动会话管理器
	return nil
}

func (s *gateApp) OnModStop(ctx context.Context) error {
	sessions := logic.SessionMgr().All()
	for _, pid := range sessions {
		StopSession(pid, gerror.New("gateway service stop"))
	}
	return nil
}

func SpawnSession(ep endpoint.Endpoint) (gxyactor.PID, error) {
	return gxyactor.SpawnFunc(func() actor.Actor {
		return logic.NewSession(ep)
	})
}

func StopSession(pid gxyactor.PID, err error) error {
	return gxyactor.Send(pid, &pb.ActorStop{
		Reason: err.Error(),
	})
}
