package account

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	"gserver/core/gxyutil"
	"gserver/src/apps/account/logic"
	"gserver/src/lib/gatetoken"

	"github.com/gogf/gf/v2/frame/g"
)

type accountService struct {
	gxyhttp.HttpService
	host string
}

type accountServiceConfig struct {
	Port    accountPortConfig    `toml:"port"`
	Gate    accountGateConfig    `toml:"gate"`
	Version accountVersionConfig `toml:"version"`
	Token   gatetoken.Config     `toml:"token"`
}

type accountPortConfig struct {
	Account int `toml:"account"`
}

type accountGateConfig struct {
	Public accountPublicGateConfig `toml:"public"`
}

type accountPublicGateConfig struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

type accountVersionConfig struct {
	Min    string `toml:"min"`
	Latest string `toml:"latest"`
}

func NewAccountService(host string) *accountService {
	return &accountService{host: host}
}

func (s *accountService) ServiceName() string {
	return "account"
}

func (s *accountService) OnModStart(ctx context.Context) error {
	cfg := &accountServiceConfig{}
	if err := gxyutil.CfgUnmarshal(ctx, g.Cfg(), cfg); err != nil {
		return err
	}
	signer, err := gatetoken.LoadSigner(cfg.Token)
	if err != nil {
		return err
	}
	handler := &logic.AccountHandler{
		Config: logic.PreloginConfig{
			MinVersion:    cfg.Version.Min,
			LatestVersion: cfg.Version.Latest,
			GateHost:      cfg.Gate.Public.Host,
			GatePort:      cfg.Gate.Public.Port,
			Env:           cfg.Token.Env,
			TokenTTL:      time.Duration(cfg.Token.ExpireSeconds) * time.Second,
			Issuer:        cfg.Token.Issuer,
		},
		Signer: signer,
	}

	svr := gxyhttp.HttpSystem().NewHttpServer(fmt.Sprintf("%s:%d", s.host, cfg.Port.Account))
	gxyhttp.SetHandler(svr, ctx, s.ServiceName(), handler)
	gxylog.Info(ctx, "account server starting")
	if err := svr.Start(); err != nil {
		return err
	}
	s.Svr = svr
	return nil
}

func (s *accountService) OnModStop(ctx context.Context) error {
	if s.Svr == nil {
		return nil
	}
	gxylog.Info(ctx, "account service stopping")
	return s.Svr.Shutdown()
}
