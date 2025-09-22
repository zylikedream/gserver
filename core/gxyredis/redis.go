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
	if err != nil {
		return err
	}
	return nil
}
