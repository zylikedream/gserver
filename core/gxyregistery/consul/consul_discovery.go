// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package consul

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/hashicorp/consul/api"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/gsvc"
)

// Search searches and returns services with specified condition.
func (r *Registry) Search(ctx context.Context, in gsvc.SearchInput) ([]gsvc.Service, error) {
	services, _, err := r.client.Health().Service(in.Name, "", true, &api.QueryOptions{
		WaitTime: time.Second * 3,
	})
	if err != nil {
		return nil, gerror.Wrap(err, "failed to get services from consul")
	}

	var result []gsvc.Service
	for _, service := range services {
		if service.Checks.AggregatedStatus() != api.HealthPassing {
			continue
		}
		metadata, err := parseServiceMeta(service)
		if err != nil {
			return nil, err
		}
		if !matchesSearch(in, service, metadata) {
			continue
		}
		result = append(result, toLocalService(service, metadata))
	}

	return result, nil
}

// parseServiceMeta 解析 consul 服务元数据;无元数据时返回 nil。
func parseServiceMeta(service *api.ServiceEntry) (map[string]any, error) {
	metaStr, ok := service.Service.Meta["data"]
	if !ok || metaStr == "" {
		return nil, nil
	}
	var metadata map[string]any
	if err := gjson.Unmarshal([]byte(metaStr), &metadata); err != nil {
		return nil, gerror.Wrap(err, "failed to unmarshal service metadata")
	}
	return metadata, nil
}

// matchesSearch 按版本与元数据过滤条件判断服务是否命中。
func matchesSearch(in gsvc.SearchInput, service *api.ServiceEntry, metadata map[string]any) bool {
	if in.Version != "" {
		if len(service.Service.Tags) == 0 || service.Service.Tags[0] != in.Version {
			return false
		}
	}
	if len(in.Metadata) > 0 {
		if metadata == nil {
			return false
		}
		for k, v := range in.Metadata {
			if mv, ok := metadata[k]; !ok || mv != v {
				return false
			}
		}
	}
	return true
}

// toLocalService 把 consul 服务条目转成本地服务实例。
func toLocalService(service *api.ServiceEntry, metadata map[string]any) gsvc.Service {
	version := ""
	if len(service.Service.Tags) > 0 {
		version = service.Service.Tags[0]
	}
	return &gsvc.LocalService{
		Head:       "",
		Deployment: "",
		Namespace:  "",
		Name:       service.Service.Service,
		Version:    version,
		Endpoints: []gsvc.Endpoint{
			gsvc.NewEndpoint(net.JoinHostPort(service.Service.Address, strconv.Itoa(service.Service.Port))),
		},
		Metadata: metadata,
	}
}
