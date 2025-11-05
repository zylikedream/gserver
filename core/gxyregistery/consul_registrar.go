package gxyregistery

import (
	"context"
	"time"

	"gserver/core/gxyregistery/consul"
	"gserver/util"

	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/os/gcfg"
)

type consulRegistery struct {
	conf *consulRegisteryConfig
	gsvc.Registry
}

type consulRegisteryConfig struct {
	Address    string        `toml:"address"`
	TTL        time.Duration `toml:"ttl"`
	RefreshTTL time.Duration `toml:"refresh_ttl"`
}

func newConsulRegistery(cfg *gcfg.Config) (*consulRegistery, error) {
	conf := &consulRegisteryConfig{
		TTL:        time.Second * 20,
		RefreshTTL: time.Second * 10,
	}
	if err := util.CfgUnmarshalKey(context.Background(), cfg, "registery.consul", conf); err != nil {
		return nil, err
	}
	regist := &consulRegistery{}
	regist.conf = conf
	registry, err := consul.New(consul.WithAddress(conf.Address),
		consul.WithHealthCheckInterval(conf.RefreshTTL),
		consul.WithTTL(conf.TTL))
	if err != nil {
		return nil, err
	}
	regist.Registry = registry
	return regist, err
}

func (r *consulRegistery) Type() string {
	return REGISTERY_TYPE_CONSUL
}
