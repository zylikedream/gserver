package gxypgx

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGXApp struct {
	gxyapp.App
	pool *pgxpool.Pool
	conf *pgxConfig
}

type pgxConfig struct {
	URL string `toml:"url"`
}

var pgxAppInstance *PGXApp

func PGX() *PGXApp {
	if pgxAppInstance.conf == nil {
		glog.Error(context.Background(), "pgx not init, miss config")
	}
	return pgxAppInstance
}

func NewPGXApp() *PGXApp {
	pgxAppInstance = &PGXApp{}
	return pgxAppInstance
}

func (p *PGXApp) OnModInit(ctx context.Context) error {
	conf := &pgxConfig{}
	if err := gxyutil.CfgUnmarshalKey(ctx, g.Cfg(), "postgres", conf); err != nil {
		return err
	}
	if conf.URL == "" {
		return nil
	}
	glog.Debugf(ctx, "conf = %v", conf)
	p.conf = conf

	poolConfig, err := pgxpool.ParseConfig(conf.URL)
	if err != nil {
		return err
	}
	p.pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return err
	}
	return nil
}

func (p *PGXApp) OnModStart(ctx context.Context) error {
	if p.pool == nil {
		return nil
	}
	if err := p.pool.Ping(ctx); err != nil {
		glog.Fatal(ctx, err)
	}
	glog.Infof(ctx, "[module]postgres start success: %s", p.conf.URL)
	return nil
}

func (p *PGXApp) OnModStop(ctx context.Context) error {
	if p.pool != nil {
		p.pool.Close()
	}
	glog.Info(ctx, "[module]postgres stop success")
	return nil
}

func (p *PGXApp) GetPool() *pgxpool.Pool {
	return p.pool
}
