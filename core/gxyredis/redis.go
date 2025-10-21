package gxyredis

import (
	"context"
	"time"

	"gserver/core/gxymodule"
	"gserver/util"

	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/os/gcfg"
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

type gxyRedis struct {
	gxymodule.ModuleBase
	conf *redisConfig
	Client
}

var redisCli *gxyRedis

func GetRedis() *gxyRedis {
	return redisCli
}

func NewRedisClient(config string) *gxyRedis {
	cfg := gcfg.Instance(config)
	conf := &redisConfig{}
	if err := util.CfgUnmarshalKey(context.Background(), cfg, "redis", conf); err != nil {
		glog.Fatal(context.Background(), err)
	}
	r := &gxyRedis{conf: conf}
	redisConf := &redis.UniversalOptions{
		Addrs:       []string{r.conf.Addr},
		Password:    r.conf.Password,
		DB:          r.conf.DB,
		DialTimeout: r.conf.Timeout,
	}
	r.Client = redis.NewUniversalClient(redisConf)
	redisCli = r
	return r
}

func (r *gxyRedis) OnModStart(ctx context.Context) error {
	// 创建一个带有超时的上下文，这里设置为5秒超时
	// 可以根据需要调整超时时间长度
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel() // 确保在函数结束时取消上下文

	if err := r.Ping(timeoutCtx).Err(); err != nil {
		return err
	}
	glog.Infof(ctx, "[module]redis start success: %s", r.conf.Addr)
	return nil
}

func (r *gxyRedis) OnModStop(ctx context.Context) error {
	if r.Client != nil {
		if err := r.Client.Close(); err != nil {
			return err
		}
	}
	glog.Info(ctx, "[module]redis stop success")
	return nil
}

func (r *gxyRedis) RunScript(ctx context.Context, script *redis.Script, keys []string, args ...any) *redis.Cmd {
	return script.Run(ctx, r.Client, keys, args...)
}
