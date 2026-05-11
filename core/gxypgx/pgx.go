package gxypgx

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/frame/g"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type PGXApp struct {
	gxyapp.App
	db   *gorm.DB
	conf *pgxConfig
}

type pgxConfig struct {
	URL string `toml:"url"`
}

var pgxAppInstance *PGXApp

func PGX() *PGXApp {
	if pgxAppInstance.db == nil {
		gxylog.Error(context.Background(), "pgx not init, miss config")
	}
	return pgxAppInstance
}

func DB() *gorm.DB {
	return pgxAppInstance.db
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
	p.conf = conf

	db, err := gorm.Open(postgres.Open(conf.URL), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Error),
	})
	if err != nil {
		return err
	}
	p.db = db
	return nil
}

func (p *PGXApp) OnModStart(ctx context.Context) error {
	if p.db == nil {
		return nil
	}
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		gxylog.Fatal(ctx, "postgres ping failed", gxylog.Err(err))
	}
	gxylog.Info(ctx, "postgres(gorm) start success", gxylog.Str("url", p.conf.URL))
	return nil
}

func (p *PGXApp) OnModStop(ctx context.Context) error {
	if p.db != nil {
		sqlDB, err := p.db.DB()
		if err == nil {
			sqlDB.Close()
		}
	}
	gxylog.Info(ctx, "postgres stop success")
	return nil
}
