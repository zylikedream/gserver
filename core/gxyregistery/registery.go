package gxyregistery

import (
	"context"
	"fmt"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/pkg/errors"
)

const (
	REGISTERY_TYPE_ETCD = "etcd"
)

type IRegistery interface {
	Register(ctx context.Context, name string, host string) error
	Search(ctx context.Context, name string) ([]ServiceNode, error)
	UnRegister(ctx context.Context, name string, host string) error
}

type registery struct {
	gsvc.Registry
}

func NewRegistery(t string, config string) (*registery, error) {
	switch t {
	case REGISTERY_TYPE_ETCD:
		r, err := newEtcdRegistery(config)
		if err != nil {
			return nil, err
		}
		return &registery{
			Registry: r,
		}, nil
	default:
		return nil, errors.Errorf("not support registery type %s", t)
	}
}

func (r *registery) getPrefix(name string) string {
	return fmt.Sprintf("/gserver/prod/service/%s/v1", name)
}

func (r *registery) Register(ctx context.Context, name string, host string) error {
	sv, _ := gjson.Marshal(map[string]string{
		"name": name,
		"host": host,
	})
	svc, err := gsvc.NewServiceWithKV(fmt.Sprintf("%s/%s", r.getPrefix(name), host), string(sv))
	if err != nil {
		return err
	}
	_, err = r.Registry.Register(ctx, svc)
	return err
}

func (r *registery) UnRegister(ctx context.Context, name string, host string) error {
	sv, _ := gjson.Marshal(map[string]string{
		"name": name,
		"host": host,
	})
	svc, err := gsvc.NewServiceWithKV(fmt.Sprintf("%s/%s", r.getPrefix(name), host), string(sv))
	if err != nil {
		return err
	}
	err = r.Registry.Deregister(ctx, svc)
	return err
}

func (r *registery) Search(ctx context.Context, name string) ([]ServiceNode, error) {
	nodes, err := r.Registry.Search(ctx, gsvc.SearchInput{
		Prefix: r.getPrefix(name),
	})
	if err != nil {
		return nil, err
	}
	serviceNodes := make([]ServiceNode, 0)
	for _, node := range nodes {
		serviceNodes = append(serviceNodes, ServiceNode{
			Name: node.GetName(),
			Node: node.GetEndpoints().String(),
		})
	}
	return serviceNodes, nil

}
