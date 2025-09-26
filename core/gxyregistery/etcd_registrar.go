package gxyregistery

import (
	"context"
	"time"

	"gserver/util"

	"github.com/gogf/gf/contrib/registry/etcd/v2"
	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/text/gstr"
)

type etcdRegistery struct {
	conf *etcdRegisteryConfig
	gsvc.Registry
}

type etcdRegisteryConfig struct {
	EtcdServers    []string      `toml:"etcd_servers"`
	UpdateInterval time.Duration `toml:"update_interval"`
}

func newEtcdRegistery(config string) (*etcdRegistery, error) {
	conf := &etcdRegisteryConfig{}
	cfg := gcfg.Instance(config)
	if err := util.CfgUnmarshalKey(context.Background(), cfg, "registery.etcd", conf); err != nil {
		return nil, err
	}
	regist := &etcdRegistery{}
	regist.conf = conf
	regist.Registry = etcd.New(gstr.Join(conf.EtcdServers, ","), etcd.Option{KeepaliveTTL: conf.UpdateInterval})
	return regist, nil
}

func (r *etcdRegistery) Type() string {
	return REGISTERY_TYPE_ETCD
}
