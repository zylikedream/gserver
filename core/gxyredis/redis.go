package gxyredis

import (
	"context"
	"time"

	"gserver/core/gxyapp"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/frame/g"

	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/redis/go-redis/v9"
)

type redisConfig struct {
	Addr     string        `toml:"addr"`
	DB       int           `toml:"db"`
	Timeout  time.Duration `toml:"dial_timeout"`
	Password string        `toml:"password"`
}

type Client redis.UniversalClient

type redisApp struct {
	gxyapp.App
	conf   *redisConfig
	client Client
}

var app *redisApp

func Redis() Client {
	if app.conf == nil {
		glog.Error(context.Background(), "redis not init, miss config")
	}
	return app.client
}

func NewRedisApp() *redisApp {
	app = &redisApp{}
	return app
}

func (r *redisApp) OnModInit(ctx context.Context) error {
	conf := &redisConfig{}
	// 判断配置文件是否存在
	if err := gxyutil.CfgUnmarshalKey(ctx, g.Cfg(), "redis", conf); err != nil {
		return err
	}
	if conf.Addr == "" {
		return nil
	}
	r.conf = conf
	r.client = redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:       []string{conf.Addr},
		Password:    conf.Password,
		DB:          conf.DB,
		DialTimeout: conf.Timeout,
	})
	return nil
}

func (r *redisApp) OnModStart(ctx context.Context) error {
	if r.client == nil {
		return nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := r.client.Ping(timeoutCtx).Err(); err != nil {
		return err
	}
	glog.Infof(ctx, "[module]redis start success: %s", r.conf.Addr)
	return nil
}

func (r *redisApp) OnModStop(ctx context.Context) error {
	if r.client != nil {
		if err := r.client.Close(); err != nil {
			return err
		}
	}
	glog.Info(ctx, "[module]redis stop success")
	return nil
}

func (r *redisApp) RunScript(ctx context.Context, script *redis.Script, keys []string, args ...any) *redis.Cmd {
	return script.Run(ctx, r.client, keys, args...)
}
