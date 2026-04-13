package gxypgx

import (
	"context"

	"gserver/core/gxyapp.go"
	"gserver/util"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgxApp struct {
	gxyapp.App
	pool *pgxpool.Pool
	conf *pgxConfig
}

type pgxConfig struct {
	URL string `toml:"url"`
}

var pgxAppInstance *pgxApp

func PGX() *pgxApp {
	return pgxAppInstance
}

func NewPGXApp() *pgxApp {
	cfg := g.Cfg()
	conf := &pgxConfig{}
	ctx := gctx.New()
	if err := util.CfgUnmarshalKey(ctx, cfg, "postgres", conf); err != nil {
		glog.Fatal(ctx, err)
	}
	pgxAppInstance = &pgxApp{conf: conf}
	return pgxAppInstance
}

func (p *pgxApp) OnModInit(ctx context.Context) error {
	poolConfig, err := pgxpool.ParseConfig(p.conf.URL)
	if err != nil {
		return err
	}
	p.pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return err
	}
	return nil
}

func (p *pgxApp) OnModStart(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		glog.Fatal(ctx, err)
	}
	glog.Infof(ctx, "[module]postgres start success: %s", p.conf.URL)
	return nil
}

func (p *pgxApp) OnModStop(ctx context.Context) error {
	if p.pool != nil {
		p.pool.Close()
	}
	glog.Info(ctx, "[module]postgres stop success")
	return nil
}

func (p *pgxApp) GetPool() *pgxpool.Pool {
	return p.pool
}
