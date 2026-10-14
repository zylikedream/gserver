package consul

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/hashicorp/consul/api"
)

func passingEntry(name, addr string, port int, meta map[string]string, tags []string) *api.ServiceEntry {
	return &api.ServiceEntry{
		Service: &api.AgentService{
			Service: name,
			Address: addr,
			Port:    port,
			Meta:    meta,
			Tags:    tags,
		},
		Checks: api.HealthChecks{&api.HealthCheck{Status: api.HealthPassing}},
	}
}

// TestServicesSkipsUnparsableEntry 单个条目的负载损坏只应跳过它,
// 而不是让整个成员列表变为空 —— 后者会让一个坏成员令本节点看不到所有健康对端。
func TestServicesSkipsUnparsableEntry(t *testing.T) {
	w := &Watcher{registry: newTestRegistry(t)}
	entries := []*api.ServiceEntry{
		passingEntry("role", "10.0.0.1", 19001, map[string]string{"data": `{"Name":"role","NodeName":"n0","NodeHost":"10.0.0.1:19001"}`}, nil),
		passingEntry("role", "10.0.0.2", 19001, map[string]string{"data": `{bad json`}, nil),
		passingEntry("role", "10.0.0.3", 19001, map[string]string{"data": `{"Name":"role","NodeName":"n2","NodeHost":"10.0.0.3:19001"}`}, nil),
	}

	got := w.Services(entries)
	if len(got) != 2 {
		t.Fatalf("Services 返回 %d 个, want 2(跳过损坏条目)", len(got))
	}
	for _, svc := range got {
		if svc.GetEndpoints()[0].Host() == "10.0.0.2" {
			t.Errorf("损坏条目未被跳过: %v", svc)
		}
	}
}

// TestServicesSkipsUnhealthy 不健康的成员不进成员列表(consul 侧健康状态)。
func TestServicesSkipsUnhealthy(t *testing.T) {
	w := &Watcher{registry: newTestRegistry(t)}
	entry := passingEntry("role", "10.0.0.1", 19001, nil, nil)
	entry.Checks = api.HealthChecks{&api.HealthCheck{Status: api.HealthCritical}}

	if got := w.Services([]*api.ServiceEntry{entry}); len(got) != 0 {
		t.Fatalf("Services 返回 %d 个, want 0(健康检查未通过)", len(got))
	}
}

// TestVersionFromTags Register 写入的 Tags 第一位是版本,消费方按位置读取。
// 写反会让版本过滤与 LocalService.Version 静默取到服务名。
func TestVersionFromTags(t *testing.T) {
	const version = "v1.2.3"
	entry := passingEntry("role", "10.0.0.1", 19001, map[string]string{
		"data": `{"Name":"role","NodeName":"n0","NodeHost":"10.0.0.1:19001"}`,
	}, []string{version, "role"})

	if !matchesSearch(gsvc.SearchInput{Name: "role", Version: version}, entry, map[string]any{}) {
		t.Error("matchesSearch 未匹配上版本")
	}
	if matchesSearch(gsvc.SearchInput{Name: "role", Version: "v9.9.9"}, entry, map[string]any{}) {
		t.Error("matchesSearch 匹配了不存在的版本")
	}

	svc := toLocalService(entry, map[string]any{})
	if svc.GetVersion() != version {
		t.Errorf("toLocalService.Version = %q, want %q", svc.GetVersion(), version)
	}
}

// TestToLocalServiceEndpoint IPv4 地址必须能原样往返。
// 注意:gsvc.NewEndpoint 内部按 ":" 切分并要求恰好两段,IPv6 字面量(无论是否
// 加方括号)都会 panic —— 框架层面不支持 IPv6,填 IPv6 的 NodeHost 会崩进程。
func TestToLocalServiceEndpoint(t *testing.T) {
	entry := passingEntry("role", "10.244.0.43", 19001, nil, nil)
	svc := toLocalService(entry, nil)
	ep := svc.GetEndpoints()[0]
	if ep.Host() != "10.244.0.43" || ep.Port() != 19001 {
		t.Errorf("endpoint = %s:%d, want 10.244.0.43:19001", ep.Host(), ep.Port())
	}
}

// TestSearchSkipsUnparsableEntry Search 遇到坏条目应跳过它并返回其余健康成员,
// 而不是整体返回错误 —— 后者会让一个坏成员令本节点看不到所有健康对端。
func TestSearchSkipsUnparsableEntry(t *testing.T) {
	body := `[
		{"Service":{"Service":"role","Address":"10.0.0.1","Port":19001,
			"Meta":{"data":"{\"Name\":\"role\",\"NodeName\":\"n0\",\"NodeHost\":\"10.0.0.1:19001\"}"}},
		 "Checks":[{"Status":"passing"}]},
		{"Service":{"Service":"role","Address":"10.0.0.2","Port":19001,
			"Meta":{"data":"{bad json"}},
		 "Checks":[{"Status":"passing"}]},
		{"Service":{"Service":"role","Address":"10.0.0.3","Port":19001,
			"Meta":{"data":"{\"Name\":\"role\",\"NodeName\":\"n2\",\"NodeHost\":\"10.0.0.3:19001\"}"}},
		 "Checks":[{"Status":"passing"}]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	client, err := api.NewClient(&api.Config{Address: addr})
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	reg := &Registry{client: client, address: addr, logger: glog.New()}

	got, err := reg.Search(context.Background(), gsvc.SearchInput{Name: "role"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Search 返回 %d 个, want 2(跳过损坏条目)", len(got))
	}
	for _, svc := range got {
		if svc.GetEndpoints()[0].Host() == "10.0.0.2" {
			t.Errorf("损坏条目未被跳过: %v", svc)
		}
	}
}
