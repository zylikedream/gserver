package gxyservice

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"gserver/core/gxynodeenv"
	"gserver/core/gxyregistery"
)

// testService 覆盖 ServiceName/Host,嵌入 Service 获取默认 Weight/Version。
type testService struct {
	Service
	name string
	host string
}

func (s *testService) ServiceName() string { return s.name }
func (s *testService) Host() string        { return s.host }

// fakeRegistry 实现 IRegistery,记录注册/注销调用。
type fakeRegistry struct {
	registerCalls   []*gxyregistery.ServiceInfo
	unregisterCalls []*gxyregistery.ServiceInfo
	registerErr     error
	hashErr         error
	hashResult      gxyregistery.HashServices
}

func (f *fakeRegistry) Register(ctx context.Context, service *gxyregistery.ServiceInfo) error {
	f.registerCalls = append(f.registerCalls, service)
	return f.registerErr
}

func (f *fakeRegistry) Search(ctx context.Context, name string) ([]*gxyregistery.ServiceInfo, error) {
	return nil, nil
}

func (f *fakeRegistry) UnRegister(ctx context.Context, service *gxyregistery.ServiceInfo) error {
	f.unregisterCalls = append(f.unregisterCalls, service)
	return nil
}

func (f *fakeRegistry) GetHashServices(ctx context.Context, name string) (gxyregistery.HashServices, error) {
	return f.hashResult, f.hashErr
}

type fakeNodeEnv struct {
	state gxyregistery.ServiceState
	err   error
}

func (f *fakeNodeEnv) State(ctx context.Context) (gxyregistery.ServiceState, error) {
	return f.state, f.err
}

type fakeSelector struct {
	picked *gxyregistery.ServiceInfo
}

func (s *fakeSelector) Select(ctx context.Context, service string, key string, services gxyregistery.HashServices) *gxyregistery.ServiceInfo {
	return s.picked
}

var _ gxynodeenv.NodeEnv = (*fakeNodeEnv)(nil)
var _ gxyregistery.IRegistery = (*fakeRegistry)(nil)
var _ gxyregistery.ServiceSelector = (*fakeSelector)(nil)

func newSvcApp(reg gxyregistery.IRegistery) *serviceApp {
	return &serviceApp{
		registry:         reg,
		nodeInstanceName: "node-1",
	}
}

func TestServiceDefaults(t *testing.T) {
	s := &Service{}
	if got := s.ServiceName(); got != "" {
		t.Errorf("ServiceName = %q, want empty", got)
	}
	if got := s.Weight(); got != DEFAULT_WEIGHT {
		t.Errorf("Weight = %d, want %d", got, DEFAULT_WEIGHT)
	}
	if got := s.Version(); got != DEFAULT_VERSION {
		t.Errorf("Version = %q, want %q", got, DEFAULT_VERSION)
	}
}

func TestLoadServiceRegistersModule(t *testing.T) {
	app := newSvcApp(&fakeRegistry{})
	svc := &testService{name: "role", host: "127.0.0.1:25011"}

	app.LoadService(context.Background(), svc)
	if got := app.GetServices(); len(got) != 1 || got[0] != svc {
		t.Fatalf("GetServices() = %v, want [svc]", got)
	}
}

func TestRegisterServices(t *testing.T) {
	reg := &fakeRegistry{}
	app := newSvcApp(reg)
	app.Services = []IService{
		&testService{name: "role", host: "127.0.0.1:25011"},
		&testService{name: "chat", host: "127.0.0.1:25041"},
	}

	if err := app.registerSevices(); err != nil {
		t.Fatalf("registerSevices: %v", err)
	}
	if len(reg.registerCalls) != 2 {
		t.Fatalf("register calls = %d, want 2", len(reg.registerCalls))
	}
	role := reg.registerCalls[0]
	if role.Name != "role" || role.NodeName != "node-1" || role.NodeHost != "127.0.0.1:25011" {
		t.Errorf("role svc = %+v, want name/role node-1 host", role)
	}
	if role.Version != DEFAULT_VERSION || role.Weight != DEFAULT_WEIGHT {
		t.Errorf("role defaults = version %q weight %d", role.Version, role.Weight)
	}
	if role.State != gxyregistery.ServiceStateServing {
		t.Errorf("state = %q, want serving (no nodeEnv)", role.State)
	}
}

func TestRegisterServicesError(t *testing.T) {
	reg := &fakeRegistry{registerErr: errors.New("register boom")}
	app := newSvcApp(reg)
	app.Services = []IService{&testService{name: "role", host: "h"}}

	if err := app.registerSevices(); err == nil {
		t.Fatal("registerSevices = nil, want error")
	}
}

func TestRefreshServiceState(t *testing.T) {
	t.Run("no_env_serving", func(t *testing.T) {
		app := newSvcApp(&fakeRegistry{})
		info := gxyregistery.NewServiceInfo("role", "node-1", "h", "v", 1)
		if !app.refreshServiceState(context.Background(), info) {
			t.Fatal("refreshServiceState = false, want true")
		}
		if info.State != gxyregistery.ServiceStateServing {
			t.Errorf("state = %q, want serving", info.State)
		}
	})

	t.Run("env_state_applied", func(t *testing.T) {
		app := newSvcApp(&fakeRegistry{})
		app.nodeEnv = &fakeNodeEnv{state: gxyregistery.ServiceStateMaintaining}
		info := gxyregistery.NewServiceInfo("role", "node-1", "h", "v", 1)
		if !app.refreshServiceState(context.Background(), info) {
			t.Fatal("refreshServiceState = false, want true")
		}
		if info.State != gxyregistery.ServiceStateMaintaining {
			t.Errorf("state = %q, want maintaining", info.State)
		}
	})

	t.Run("env_error_keeps_state", func(t *testing.T) {
		app := newSvcApp(&fakeRegistry{})
		app.nodeEnv = &fakeNodeEnv{err: errors.New("state down")}
		info := gxyregistery.NewServiceInfo("role", "node-1", "h", "v", 1)
		info.State = gxyregistery.ServiceStateServing
		if app.refreshServiceState(context.Background(), info) {
			t.Fatal("refreshServiceState = true, want false")
		}
		if info.State != gxyregistery.ServiceStateServing {
			t.Errorf("state = %q, want unchanged serving", info.State)
		}
	})
}

func TestRefreshRegisteredServices(t *testing.T) {
	t.Run("state_change_reregisters", func(t *testing.T) {
		reg := &fakeRegistry{}
		app := newSvcApp(reg)
		app.Services = []IService{&testService{name: "role", host: "h"}}
		if err := app.registerSevices(); err != nil {
			t.Fatalf("registerSevices: %v", err)
		}
		// 初始 serving;随后节点状态变为 draining。
		app.nodeEnv = &fakeNodeEnv{state: gxyregistery.ServiceStateDraining}

		app.refreshRegisteredServices(context.Background())
		if len(reg.registerCalls) != 2 {
			t.Fatalf("register calls = %d, want 2 (initial + state change)", len(reg.registerCalls))
		}
		if got := reg.registerCalls[1].State; got != gxyregistery.ServiceStateDraining {
			t.Errorf("re-registered state = %q, want draining", got)
		}
	})

	t.Run("no_change_no_reregister", func(t *testing.T) {
		reg := &fakeRegistry{}
		app := newSvcApp(reg)
		app.Services = []IService{&testService{name: "role", host: "h"}}
		if err := app.registerSevices(); err != nil {
			t.Fatalf("registerSevices: %v", err)
		}
		app.refreshRegisteredServices(context.Background())
		if len(reg.registerCalls) != 1 {
			t.Errorf("register calls = %d, want 1 (no state change)", len(reg.registerCalls))
		}
	})

	t.Run("state_error_no_reregister", func(t *testing.T) {
		reg := &fakeRegistry{}
		app := newSvcApp(reg)
		app.Services = []IService{&testService{name: "role", host: "h"}}
		if err := app.registerSevices(); err != nil {
			t.Fatalf("registerSevices: %v", err)
		}
		app.nodeEnv = &fakeNodeEnv{err: errors.New("state down")}
		app.refreshRegisteredServices(context.Background())
		if len(reg.registerCalls) != 1 {
			t.Errorf("register calls = %d, want 1 (state refresh failed)", len(reg.registerCalls))
		}
	})
}

func TestGetServiceInfo(t *testing.T) {
	picked := gxyregistery.NewServiceInfo("role", "node-1", "h", "v", 1)
	t.Run("selects", func(t *testing.T) {
		app := newSvcApp(&fakeRegistry{hashResult: gxyregistery.HashServices{}})
		got := app.GetServiceInfo(context.Background(), "role", "key", &fakeSelector{picked: picked})
		if got != picked {
			t.Errorf("GetServiceInfo = %v, want picked", got)
		}
	})
	t.Run("registry_error_returns_nil", func(t *testing.T) {
		app := newSvcApp(&fakeRegistry{hashErr: errors.New("hash boom")})
		if got := app.GetServiceInfo(context.Background(), "role", "key", &fakeSelector{picked: picked}); got != nil {
			t.Errorf("GetServiceInfo = %v, want nil on error", got)
		}
	})
}

func TestGetAddressByNodeName(t *testing.T) {
	svcA := gxyregistery.NewServiceInfo("role", "node-1", "127.0.0.1:25011", "v1", 1)
	svcB := gxyregistery.NewServiceInfo("role", "node-2", "127.0.0.1:25012", "v1", 1)

	t.Run("hit", func(t *testing.T) {
		app := newSvcApp(&fakeRegistry{hashResult: gxyregistery.HashServices{
			ServiceInfos: []*gxyregistery.ServiceInfo{svcA, svcB},
		}})
		if got := app.GetAddressByNodeName(context.Background(), "role", "node-2"); got != "127.0.0.1:25012" {
			t.Errorf("GetAddressByNodeName = %q, want 127.0.0.1:25012", got)
		}
	})
	t.Run("miss", func(t *testing.T) {
		app := newSvcApp(&fakeRegistry{hashResult: gxyregistery.HashServices{
			ServiceInfos: []*gxyregistery.ServiceInfo{svcA},
		}})
		if got := app.GetAddressByNodeName(context.Background(), "role", "node-9"); got != "" {
			t.Errorf("GetAddressByNodeName = %q, want empty", got)
		}
	})
	t.Run("registry_error", func(t *testing.T) {
		app := newSvcApp(&fakeRegistry{hashErr: errors.New("hash boom")})
		if got := app.GetAddressByNodeName(context.Background(), "role", "node-1"); got != "" {
			t.Errorf("GetAddressByNodeName = %q, want empty on error", got)
		}
	})
}

func TestOnModStopUnregisters(t *testing.T) {
	reg := &fakeRegistry{}
	app := newSvcApp(reg)
	app.Services = []IService{
		&testService{name: "role", host: "h1"},
		&testService{name: "chat", host: "h2"},
	}
	if err := app.registerSevices(); err != nil {
		t.Fatalf("registerSevices: %v", err)
	}
	app.nodeEnv = &fakeNodeEnv{state: gxyregistery.ServiceStateServing}

	if err := app.OnModStop(context.Background()); err != nil {
		t.Fatalf("OnModStop: %v", err)
	}
	if len(reg.unregisterCalls) != 2 {
		t.Errorf("unregister calls = %d, want 2", len(reg.unregisterCalls))
	}
	if !reflect.DeepEqual(reg.unregisterCalls[0], reg.registerCalls[0]) {
		t.Errorf("unregistered svc %+v, want registered %+v", reg.unregisterCalls[0], reg.registerCalls[0])
	}
}
