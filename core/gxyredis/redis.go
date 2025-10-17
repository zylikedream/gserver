package gxyredis

import (
	"context"
	"time"

	"gserver/core/gxymodule"
	"gserver/util"

	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/glog"
)

type redisConfig struct {
	Addr     string        `toml:"addr"`
	DB       int           `toml:"db"`
	Timeout  time.Duration `toml:"dial_timeout"`
	Password string        `toml:"password"`
}

type gxyRedis struct {
	gxymodule.Module
	conf *redisConfig
	*gredis.Redis
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
	redisCli = r
	return r
}
func (r *gxyRedis) OnInit(ctx context.Context) error {
	redisConf := &gredis.Config{}
	redisConf.Address = r.conf.Addr
	redisConf.Pass = r.conf.Password
	redisConf.Db = r.conf.DB
	redisConf.DialTimeout = r.conf.Timeout
	var err error
	r.Redis, err = gredis.New(redisConf)
	r.Redis.Client()
	if err != nil {
		return err
	}
	return nil
}

func (r *gxyRedis) OnStart(ctx context.Context) error {
	// 创建一个带有超时的上下文，这里设置为5秒超时
	// 可以根据需要调整超时时间长度
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel() // 确保在函数结束时取消上下文

	if _, err := r.Do(timeoutCtx, "PING"); err != nil {
		return err
	}
	glog.Infof(ctx, "[module]redis start success: %s", r.conf.Addr)
	return nil
}
