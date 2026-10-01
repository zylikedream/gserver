package gateway

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxyapp"
	"gserver/core/gxynet"
	"gserver/core/gxynet/endpoint"
	"gserver/protocol/pb"
	"gserver/src/apps/gateway/internal/logic"
	"gserver/src/lib"
	"gserver/src/lib/gatetoken"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

// gateApp 是网关应用的组合根:组装网络端点、会话依赖与令牌校验。
type gateApp struct {
	gxyapp.App
	sessions *gxyactor.ActorMgr
}

func NewGateApp() *gateApp {
	return &gateApp{}
}

func (s *gateApp) OnModInit(ctx context.Context) error {
	loginCfg, err := logic.LoadLoginLimitConfig(ctx, g.Cfg())
	if err != nil {
		return err
	}
	loginLimiter, err := logic.NewLoginLimiter(loginCfg)
	if err != nil {
		return err
	}

	tokenCfg, err := gatetoken.LoadConfigFromGF(ctx)
	if err != nil {
		return err
	}
	signer, err := gatetoken.LoadSigner(*tokenCfg)
	if err != nil {
		return err
	}
	s.sessions = gxyactor.NewActorMgr("session_mgr")
	deps := logic.SessionDeps{
		// 时钟在组装根给:签验器不持有时间来源。
		VerifyToken: func(token string) (*gatetoken.Claims, error) {
			return signer.Verify(token, time.Now())
		},
		Login:        loginLimiter,
		ActivateRole: lib.ActivateRole,
		Sessions:     s.sessions,
	}
	network := gxynet.NewNetwork(g.Cfg(), NewGateHandler(deps))
	if err := s.AddModule(ctx, network); err != nil {
		return err
	}
	return nil
}

func (s *gateApp) OnModStart(ctx context.Context) error {
	return nil
}

func (s *gateApp) OnModStop(ctx context.Context) error {
	sessions := s.sessions.All()
	for _, pid := range sessions {
		_ = stopSession(pid, gerror.New("gateway service stop"))
	}
	return nil
}

func spawnSession(ep endpoint.Endpoint, d logic.SessionDeps) (gxyactor.PID, error) {
	return gxyactor.Spawn("session", func() gxyactor.Business {
		return logic.NewSession(ep, d)
	})
}

// stopSession 请求会话终止。err=nil 表示正常结束(连接干净关闭):
// 原因经协议传字符串,空串即"没有原因",运行时据此走正常终止而不是记异常。
func stopSession(pid gxyactor.PID, err error) error {
	reason := ""
	if err != nil {
		reason = err.Error()
	}
	return gxyactor.Send(context.Background(), pid, &pb.ActorStop{
		Reason: reason,
	})
}
