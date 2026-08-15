package gxypgx

import (
	"context"
	"time"

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
	URL             string `toml:"url"`
	MaxOpenConns    int    `toml:"max_open_conns"`
	MaxIdleConns    int    `toml:"max_idle_conns"`
	ConnMaxLifetime int    `toml:"conn_max_lifetime"` // minutes
}

var pgxAppInstance *PGXApp

func PGX() *PGXApp {
	if pgxAppInstance == nil || pgxAppInstance.db == nil {
		gxylog.Error(context.Background(), "pgx not init, miss config")
	}
	return pgxAppInstance
}

// DB 返回全局数据库连接;未初始化时返回 nil(生产由模块启动初始化,测试兜底不 panic)。
func DB() *gorm.DB {
	if pgxAppInstance == nil || pgxAppInstance.db == nil {
		gxylog.Error(context.Background(), "pgx not init, miss config")
		return nil
	}
	return pgxAppInstance.db
}

func NewPGXApp() *PGXApp {
	pgxAppInstance = &PGXApp{}
	return pgxAppInstance
}

func (p *PGXApp) OnModInit(ctx context.Context) error {
	conf := &pgxConfig{
		MaxOpenConns:    30,
		MaxIdleConns:    10,
		ConnMaxLifetime: 30,
	}
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
	if err := db.Use(&metricsPlugin{}); err != nil {
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
	sqlDB.SetMaxOpenConns(p.conf.MaxOpenConns)
	sqlDB.SetMaxIdleConns(p.conf.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(p.conf.ConnMaxLifetime) * time.Minute)

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
			_ = sqlDB.Close()
		}
	}
	gxylog.Info(ctx, "postgres stop success")
	return nil
}
