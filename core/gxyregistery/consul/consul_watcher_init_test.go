package consul

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/os/glog"
	"github.com/hashicorp/consul/api"
)

// TestNewWatcherReturnsInitError 建立 watcher 的初始化失败必须同步返回错误。
// 若像原来那样在协程里静默 return,plan 永不启动、eventChan 永无信号,而
// Proceed 只在 Close 时返回 error —— 消费方无法感知死亡,该服务名的发现数据
// 会永久冻结且不会重建。
//
// 用返回 500 的本地 HTTP 桩作为 consul 的边界替身,无需真实服务。
func TestNewWatcherReturnsInitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	client, err := api.NewClient(&api.Config{Address: addr})
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	reg := &Registry{client: client, address: addr}

	watcher, err := newWatcher(reg, "role")
	if err == nil {
		t.Fatal("newWatcher err = nil, want 初始化失败的错误")
	}
	if watcher != nil {
		t.Fatalf("newWatcher watcher = %v, want nil", watcher)
	}
}

// newTestRegistry 提供带 logger 的 Registry,供不依赖 consul 的纯函数测试使用。
func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	client, err := api.NewClient(&api.Config{Address: "127.0.0.1:8500"})
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	return &Registry{client: client, address: "127.0.0.1:8500", logger: glog.New()}
}
