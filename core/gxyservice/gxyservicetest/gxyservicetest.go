// Package gxyservicetest 提供跨服务调用的测试替身。
//
// 形态与 gxyactortest 一致:保留真实边界——真实 ghttp 服务器、真实服务发现查询、
// 真实 HTTP 客户端——只把「对面是谁」换成测试进程内的假服务。
// 替身按服务的路由与响应体说话,所以对端契约漂移时测试失败;
// 生产代码不需要为测试留任何位置。
//
// 用法:
//
//	reg := gxyservicetest.Install(t)
//	reg.Serve(t, "friend", &fakeFriendHandler{...})
//	// 此后业务代码的 gxyhttp.PostService(ctx, "friend", ...) 打到假服务
package gxyservicetest

import (
	"context"
	"sync"
	"testing"

	"gserver/core/gxyhttp"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
)

// 假服务登记用的节点名:与生产节点名无关,只要求同一服务下唯一。
const testNodeName = "gxyservicetest"

// Registry 是 gxyregistery.IRegistery 的内存实现,按服务名收集本进程内的假服务。
type Registry struct {
	mu   sync.Mutex
	svcs map[string][]*gxyregistery.ServiceInfo
}

// Install 在组装处装上内存服务注册表,并返回它供 Serve 登记假服务。
func Install(t testing.TB) *Registry {
	t.Helper()
	reg := &Registry{}
	gxyservice.NewServiceApp(testNodeName, reg)
	return reg
}

// Serve 启动一个真实 HTTP 服务器,按 name 分组绑定 handler,并把地址登记为名为 name 的服务。
//
// handler 与生产服务同形(ghttp 结构体绑定,g.Meta 声明 path/method),
// 响应体按服务契约返回。返回服务地址,测试结束自动关闭。
func (r *Registry) Serve(t testing.TB, name string, handler any) string {
	t.Helper()
	svr := gxyhttp.NewHttpApp().NewHttpServer("127.0.0.1:0")
	gxyhttp.SetHandler(svr, context.Background(), name, handler)
	if err := svr.Start(); err != nil {
		t.Fatalf("start fake service %s: %v", name, err)
	}
	t.Cleanup(func() { _ = svr.Shutdown() })

	host := svr.GetListenedAddress()
	svc := gxyregistery.NewServiceInfo(name, testNodeName, host, gxyservice.DEFAULT_VERSION, gxyservice.DEFAULT_WEIGHT)
	if err := r.Register(context.Background(), svc); err != nil {
		t.Fatalf("register fake service %s: %v", name, err)
	}
	return host
}

func (r *Registry) Register(_ context.Context, service *gxyregistery.ServiceInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.svcs == nil {
		r.svcs = make(map[string][]*gxyregistery.ServiceInfo)
	}
	for i, cur := range r.svcs[service.Name] {
		if cur.NodeName == service.NodeName {
			r.svcs[service.Name][i] = service
			return nil
		}
	}
	r.svcs[service.Name] = append(r.svcs[service.Name], service)
	return nil
}

func (r *Registry) UnRegister(_ context.Context, service *gxyregistery.ServiceInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.svcs[service.Name]
	for i, cur := range list {
		if cur.NodeName == service.NodeName {
			r.svcs[service.Name] = append(list[:i], list[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r *Registry) Search(_ context.Context, name string) ([]*gxyregistery.ServiceInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.svcs[name], nil
}

// GetHashServices 返回该服务名下的全部登记项。
// Hash 留空:一致性哈希选择器会因此每次重建环——测试里服务集合不变,代价可忽略。
func (r *Registry) GetHashServices(ctx context.Context, name string) (gxyregistery.HashServices, error) {
	svcs, err := r.Search(ctx, name)
	if err != nil {
		return gxyregistery.HashServices{}, err
	}
	return gxyregistery.HashServices{ServiceInfos: svcs}, nil
}

var _ gxyregistery.IRegistery = (*Registry)(nil)
