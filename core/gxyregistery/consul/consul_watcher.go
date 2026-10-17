// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package consul

import (
	"context"
	"encoding/json"
	"net"
	"sort"
	"strconv"
	"sync"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/gsvc"
)

// Watcher watches the service changes.
type Watcher struct {
	registry  *Registry      // The registry instance
	key       string         // The service name to watch
	closeChan chan struct{}  // Channel for closing
	eventChan chan struct{}  // Channel for notifying changes
	mu        sync.RWMutex   // Mutex for thread safety
	plan      *watch.Plan    // The watch plan
	services  []gsvc.Service // Current services
}

// newWatcher 建立 watcher 并启动 watch plan。
// 初始化查询同步执行:失败必须让调用方拿到错误 —— 若异步吞掉,plan 永远不会
// 启动、eventChan 永远不会有信号,而 Proceed 只在 Close 时返回 error,
// 消费方将无法感知死亡,该服务名的发现数据会永久冻结。
func newWatcher(registry *Registry, key string) (*Watcher, error) {
	w := &Watcher{
		registry:  registry,
		key:       key,
		closeChan: make(chan struct{}),
		eventChan: make(chan struct{}, 1),
	}

	entries, _, err := registry.client.Health().Service(key, "", true, &api.QueryOptions{})
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.services = w.Services(entries)
	w.mu.Unlock()

	plan, err := watch.Parse(map[string]any{
		"type":    "service",
		"service": key,
	})
	if err != nil {
		return nil, err
	}

	w.mu.Lock()
	w.plan = plan
	w.mu.Unlock()

	plan.Handler = w.onServiceChange
	go w.runPlan(plan)

	return w, nil
}

// onServiceChange 是 consul watch 回调:更新本地服务列表并通知等待者。
func (w *Watcher) onServiceChange(_ uint64, data any) {
	select {
	case <-w.closeChan:
		return
	default:
	}

	entries, ok := data.([]*api.ServiceEntry)
	if !ok {
		return
	}
	w.mu.Lock()
	w.services = w.Services(entries)
	w.mu.Unlock()

	select {
	case w.eventChan <- struct{}{}:
	default:
	}
}

// runPlan 在后台运行 watch plan,退出时清理 plan。
func (w *Watcher) runPlan(plan *watch.Plan) {
	defer func() {
		w.mu.Lock()
		if w.plan != nil {
			w.plan.Stop()
			w.plan = nil
		}
		w.mu.Unlock()
	}()

	_ = plan.Run(w.registry.GetAddress())
}

// Proceed returns current services and waits for the next service change.
func (w *Watcher) Proceed() ([]gsvc.Service, error) {
	// Check if watcher is closed
	select {
	case <-w.closeChan:
		return nil, gerror.New("watcher closed")
	default:
	}

	// Wait for changes
	select {
	case <-w.closeChan:
		return nil, gerror.New("watcher closed")
	case <-w.eventChan:
		w.mu.RLock()
		services := w.services
		w.mu.RUnlock()
		return services, nil
	}
}

// Close closes the watcher.
func (w *Watcher) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.closeChan:
		return nil
	default:
		close(w.closeChan)
		if w.plan != nil {
			w.plan.Stop()
			w.plan = nil
		}
		return nil
	}
}

// Services 把 consul 条目转成服务实例,只保留健康且元数据可解析的条目。
// 单个条目损坏只跳过它并记日志:整体返回错误会让一个坏成员的负载
// 令本节点看不到所有健康对端。
func (w *Watcher) Services(entries []*api.ServiceEntry) []gsvc.Service {
	var services []gsvc.Service
	for _, entry := range entries {
		if entry.Checks.AggregatedStatus() != api.HealthPassing {
			continue
		}

		metadata := make(map[string]any)
		if entry.Service.Meta != nil {
			if metaStr, ok := entry.Service.Meta["data"]; ok && metaStr != "" {
				if err := json.Unmarshal([]byte(metaStr), &metadata); err != nil {
					w.registry.logger.Warningf(context.Background(),
						"skip service %s: unmarshal metadata failed: %v", entry.Service.Service, err)
					continue
				}
			}
		}

		// version 优先取 meta,退回 tags[0](Register 保证 tags[0] 是版本)
		version := ""
		if v, ok := entry.Service.Meta["version"]; ok {
			version = v
		} else if len(entry.Service.Tags) > 0 {
			version = entry.Service.Tags[0]
		}

		services = append(services, &gsvc.LocalService{
			Name:     entry.Service.Service,
			Version:  version,
			Metadata: metadata,
			Endpoints: []gsvc.Endpoint{
				gsvc.NewEndpoint(net.JoinHostPort(entry.Service.Address, strconv.Itoa(entry.Service.Port))),
			},
		})
	}

	// 排序保证同一份成员集产生同样的顺序(消费方依赖顺序的确定性)
	if len(services) > 0 {
		sort.Slice(services, func(i, j int) bool {
			return services[i].GetVersion() < services[j].GetVersion()
		})
	}
	return services
}
