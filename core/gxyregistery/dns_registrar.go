package gxyregistery

import (
	"context"
	"time"

	"gserver/core/gxyregistery/dns"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/os/gcfg"
)

type dnsRegisteryConfig struct {
	Domain   string `toml:"domain"`
	Interval int    `toml:"interval"`
}

type dnsRegistery struct {
	gsvc.Registry
}

func newDNSRegistery(cfg *gcfg.Config) (*dnsRegistery, error) {
	conf := &dnsRegisteryConfig{
		Interval: 10,
	}
	if err := gxyutil.CfgUnmarshalKey(context.Background(), cfg, "registery.dns", conf); err != nil {
		return nil, err
	}
	return &dnsRegistery{
		Registry: dns.New(conf.Domain, time.Duration(conf.Interval)*time.Second),
	}, nil
}

func (r *dnsRegistery) Type() string {
	return REGISTERY_TYPE_DNS
}
