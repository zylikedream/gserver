package gxyregistery

import (
	"context"
	"time"

	"gserver/core/gxyregistery/redis"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/os/gcfg"
)

type redisRegisteryConfig struct {
	Interval int `toml:"interval"`
}

type redisRegistery struct {
	gsvc.Registry
}

func newRedisRegistery(cfg *gcfg.Config) (*redisRegistery, error) {
	conf := &redisRegisteryConfig{
		Interval: 10,
	}
	if err := gxyutil.CfgUnmarshalKey(context.Background(), cfg, "registery.redis", conf); err != nil {
		return nil, err
	}
	return &redisRegistery{
		Registry: redis.New(time.Duration(conf.Interval) * time.Second),
	}, nil
}

func (r *redisRegistery) Type() string {
	return REGISTERY_TYPE_REDIS
}
