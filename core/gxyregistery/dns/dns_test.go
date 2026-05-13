package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"gserver/core/gxyredis"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/gogf/gf/v2/net/gsvc"
	"github.com/redis/go-redis/v9"
)

// mockRedisClient overrides only the Redis commands used by Registry.
type mockRedisClient struct {
	redis.UniversalClient
	hsetFn    func(ctx context.Context, key string, values ...any) *redis.IntCmd
	hdelFn    func(ctx context.Context, key string, fields ...string) *redis.IntCmd
	hgetAllFn func(ctx context.Context, key string) *redis.MapStringStringCmd
}

func (m *mockRedisClient) HSet(ctx context.Context, key string, values ...any) *redis.IntCmd {
	return m.hsetFn(ctx, key, values...)
}

func (m *mockRedisClient) HDel(ctx context.Context, key string, fields ...string) *redis.IntCmd {
	return m.hdelFn(ctx, key, fields...)
}

func (m *mockRedisClient) HGetAll(ctx context.Context, key string) *redis.MapStringStringCmd {
	return m.hgetAllFn(ctx, key)
}

// patchRedis patches gxyredis.Redis to return the mock client.
func patchRedis(mock *mockRedisClient) *gomonkey.Patches {
	return gomonkey.ApplyFunc(gxyredis.Redis, func() gxyredis.Client {
		return gxyredis.Client(mock)
	})
}

// patchDNS patches net.DefaultResolver.LookupHost.
// fn must be func(*net.Resolver, context.Context, string) ([]string, error).
func patchDNS(fn any) *gomonkey.Patches {
	return gomonkey.ApplyMethod(reflect.TypeOf(net.DefaultResolver), "LookupHost", fn)
}

// ---- extractPodName ----

func TestExtractPodName(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"normal", "gserver-game-0@uid-role:0.0.0.0:10090", "game-0"},
		{"different_name", "gserver-game-1@uid-friend:10.0.1.5:10090", "game-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &dnsService{key: tt.key}
			if got := extractPodName(svc); got != tt.want {
				t.Errorf("extractPodName(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestExtractPodName_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"empty_key", "", ""},
		{"no_dash", "short", ""},
		{"no_at", "gserver-game0-role:0.0.0.0:10090", "game0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &dnsService{key: tt.key}
			if got := extractPodName(svc); got != tt.want {
				t.Errorf("extractPodName(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// ---- gsvc.Service stub for tests ----

type testService struct {
	name     string
	nodeName string
	host     string
	version  string
	weight   int
}

func (s *testService) GetName() string            { return s.name }
func (s *testService) GetVersion() string          { return s.version }
func (s *testService) GetKey() string {
	return "gserver-" + s.nodeName + "-" + s.name + ":" + s.host
}
func (s *testService) GetValue() string {
	return fmt.Sprintf(`{"Name":"%s","NodeName":"%s","Version":"%s","Weight":%d,"NodeHost":"%s"}`,
		s.name, s.nodeName, s.version, s.weight, s.host)
}
func (s *testService) GetPrefix() string            { return gsvc.DefaultSeparator + s.name }
func (s *testService) GetMetadata() gsvc.Metadata   { return nil }
func (s *testService) GetEndpoints() gsvc.Endpoints { return gsvc.NewEndpoints(s.host) }

func makeIntCmd(val int64) *redis.IntCmd {
	cmd := redis.NewIntCmd(context.Background())
	cmd.SetVal(val)
	return cmd
}

func makeMapCmd(data map[string]string) *redis.MapStringStringCmd {
	cmd := redis.NewMapStringStringCmd(context.Background())
	cmd.SetVal(data)
	return cmd
}

// ---- Register & Deregister ----

func TestRegistry_RegisterAndDeregister(t *testing.T) {
	mock := &mockRedisClient{}
	store := make(map[string]string) // key:field → json

	mock.hsetFn = func(_ context.Context, key string, values ...any) *redis.IntCmd {
		store[key+":"+values[0].(string)] = values[1].(string)
		return makeIntCmd(1)
	}
	mock.hdelFn = func(_ context.Context, key string, fields ...string) *redis.IntCmd {
		delete(store, key+":"+fields[0])
		return makeIntCmd(1)
	}

	p := patchRedis(mock)
	defer p.Reset()

	r := New("", 0)
	svc := &testService{
		name:     "role",
		nodeName: "game-0@abc123",
		host:     "0.0.0.0:10090",
	}

	_, err := r.Register(context.Background(), svc)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	expectedStoreKey := "gserver:dns:svc:role:game-0"
	if _, ok := store[expectedStoreKey]; !ok {
		t.Errorf("expected store key %s to exist", expectedStoreKey)
	}

	if err := r.Deregister(context.Background(), svc); err != nil {
		t.Fatalf("Deregister failed: %v", err)
	}
	if _, ok := store[expectedStoreKey]; ok {
		t.Errorf("expected store key %s to be removed after Deregister", expectedStoreKey)
	}
}

// ---- Search ----

func TestRegistry_Search(t *testing.T) {
	mock := &mockRedisClient{}
	r := New("svc.cluster.local", 0)

	svcJSON := `{"Name":"role","NodeName":"game-0@abc123","Version":"1.0","Weight":0,"NodeHost":"0.0.0.0:10090"}`
	mock.hgetAllFn = func(_ context.Context, key string) *redis.MapStringStringCmd {
		return makeMapCmd(map[string]string{"game-0": svcJSON})
	}

	p1 := patchRedis(mock)
	defer p1.Reset()
	p2 := patchDNS(func(_ *net.Resolver, _ context.Context, host string) ([]string, error) {
		return []string{"10.0.1.5"}, nil
	})
	defer p2.Reset()

	services, err := r.Search(context.Background(), gsvc.SearchInput{Name: "role"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}

	svc := services[0]
	if svc.GetName() != "role" {
		t.Errorf("expected name 'role', got %q", svc.GetName())
	}
	if svc.GetVersion() != "1.0" {
		t.Errorf("expected version '1.0', got %q", svc.GetVersion())
	}

	// GetValue() must contain the resolved IP (not 0.0.0.0)
	val := svc.GetValue()
	if !strings.Contains(val, "10.0.1.5") {
		t.Errorf("GetValue() should contain resolved IP, got: %s", val)
	}

	ep := svc.GetEndpoints()
	if len(ep) == 0 || ep[0].Host() != "10.0.1.5" || ep[0].Port() != 10090 {
		t.Errorf("expected '10.0.1.5:10090', got %s:%d", ep[0].Host(), ep[0].Port())
	}
}

func TestRegistry_Search_Empty(t *testing.T) {
	mock := &mockRedisClient{}
	mock.hgetAllFn = func(_ context.Context, key string) *redis.MapStringStringCmd {
		return makeMapCmd(nil)
	}

	p := patchRedis(mock)
	defer p.Reset()

	r := New("", 0)
	services, err := r.Search(context.Background(), gsvc.SearchInput{Name: "nonexistent"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected 0 services, got %d", len(services))
	}
}

func TestRegistry_Search_DNSFallback(t *testing.T) {
	mock := &mockRedisClient{}
	r := New("svc.cluster.local", 0)

	svcJSON := `{"Name":"role","NodeName":"game-0@abc123","Version":"","Weight":0,"NodeHost":"0.0.0.0:10090"}`
	mock.hgetAllFn = func(_ context.Context, key string) *redis.MapStringStringCmd {
		return makeMapCmd(map[string]string{"game-0": svcJSON})
	}

	p1 := patchRedis(mock)
	defer p1.Reset()
	p2 := patchDNS(func(_ *net.Resolver, _ context.Context, host string) ([]string, error) {
		return nil, errors.New("dns timeout")
	})
	defer p2.Reset()

	services, err := r.Search(context.Background(), gsvc.SearchInput{Name: "role"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}

	// DNS failed → keep original host
	ep := services[0].GetEndpoints()
	if ep[0].Host() != "0.0.0.0" {
		t.Errorf("expected fallback host '0.0.0.0', got %q", ep[0].Host())
	}

	val := services[0].GetValue()
	if !strings.Contains(val, "0.0.0.0") {
		t.Errorf("GetValue() should keep original host on DNS failure, got: %s", val)
	}
}

// ---- serviceFromJSON ----

func TestServiceFromJSON_Minimal(t *testing.T) {
	jsonStr := `{"Name":"friend","NodeName":"game-1@def456","Version":"","Weight":0,"NodeHost":"0.0.0.0:10091"}`

	p := patchDNS(func(_ *net.Resolver, _ context.Context, host string) ([]string, error) {
		return []string{"10.0.1.6"}, nil
	})
	defer p.Reset()

	svc, err := serviceFromJSON(jsonStr, "game-1", "svc.cluster.local")
	if err != nil {
		t.Fatalf("serviceFromJSON failed: %v", err)
	}
	if svc.GetName() != "friend" {
		t.Errorf("expected name 'friend', got %q", svc.GetName())
	}

	ep := svc.GetEndpoints()
	if len(ep) == 0 || ep[0].Host() != "10.0.1.6" || ep[0].Port() != 10091 {
		t.Errorf("expected '10.0.1.6:10091', got %s:%d", ep[0].Host(), ep[0].Port())
	}
}

func TestServiceFromJSON_InvalidJSON(t *testing.T) {
	_, err := serviceFromJSON("{bad json", "game-0", "svc.cluster.local")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestServiceFromJSON_EmptyPodName(t *testing.T) {
	jsonStr := `{"Name":"role","NodeName":"game-0@abc","NodeHost":"0.0.0.0:10090"}`
	_, err := serviceFromJSON(jsonStr, "", "svc.cluster.local")
	if err == nil {
		t.Fatal("expected error for empty pod name")
	}
}
