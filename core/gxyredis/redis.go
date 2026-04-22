package gxyredis

import (
	"context"
	"time"

	"gserver/core/gxyapp"
	"gserver/util"

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
	return app.client
}

func NewRedisApp() *redisApp {
	cfg := g.Cfg()
	conf := &redisConfig{}
	if err := util.CfgUnmarshalKey(context.Background(), cfg, "redis", conf); err != nil {
		glog.Fatal(context.Background(), err)
	}
	r := &redisApp{conf: conf}
	redisConf := &redis.UniversalOptions{
		Addrs:       []string{r.conf.Addr},
		Password:    r.conf.Password,
		DB:          r.conf.DB,
		DialTimeout: r.conf.Timeout,
	}
	r.client = redis.NewUniversalClient(redisConf)
	app = r
	return r
}

func (r *redisApp) OnModStart(ctx context.Context) error {
	// 创建一个带有超时的上下文，这里设置为5秒超时
	// 可以根据需要调整超时时间长度
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel() // 确保在函数结束时取消上下文

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
