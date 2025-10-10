package gxyregistery

import (
	"context"
	"time"

	"gserver/util"

	"github.com/gogf/gf/contrib/registry/etcd/v2"
	"github.com/gogf/gf/v2/errors/gerror"
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
	if err := testEtcdConnection(regist.Registry); err != nil {
		return nil, err
	}
	return regist, nil
}

// 测试etcd连接的示例代码
func testEtcdConnection(registry gsvc.Registry) error {
	// 创建一个带超时的上下文，避免连接超时导致程序长时间阻塞
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 初始化etcd注册中心
	// 也可以尝试搜索服务来验证连接
	_, err := registry.Search(ctx, gsvc.SearchInput{
		Prefix: "test",
	})
	if err != nil {
		return gerror.New("etcd connect server failed")
	}

	return nil
}

func (r *etcdRegistery) Type() string {
	return REGISTERY_TYPE_ETCD
}
