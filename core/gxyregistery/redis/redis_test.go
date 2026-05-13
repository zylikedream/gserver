package redis

import (
	"context"
	"fmt"
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
	doFn      func(ctx context.Context, args ...any) *redis.Cmd
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

func (m *mockRedisClient) Do(ctx context.Context, args ...any) *redis.Cmd {
	if m.doFn != nil {
		return m.doFn(ctx, args...)
	}
	// Default: return OK for HEXPIRE
	cmd := redis.NewCmd(ctx)
	cmd.SetVal("OK")
	return cmd
}

// patchRedis patches gxyredis.Redis to return the mock client.
func patchRedis(mock *mockRedisClient) *gomonkey.Patches {
	return gomonkey.ApplyFunc(gxyredis.Redis, func() gxyredis.Client {
		return gxyredis.Client(mock)
	})
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

	r := New(0)
	svc := &testService{
		name:     "role",
		nodeName: "game-0@abc123",
		host:     "0.0.0.0:10090",
	}

	_, err := r.Register(context.Background(), svc)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	expectedStoreKey := "gserver:svc:role:gserver-game-0@abc123-role:0.0.0.0:10090"
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
	r := New(0)

	svcJSON := `{"Name":"role","NodeName":"game-0@abc123","Version":"1.0","Weight":0,"NodeHost":"0.0.0.0:10090"}`
	mock.hgetAllFn = func(_ context.Context, key string) *redis.MapStringStringCmd {
		return makeMapCmd(map[string]string{"game-0": svcJSON})
	}

	p := patchRedis(mock)
	defer p.Reset()

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

	// No DNS resolution: NodeHost stays as-is from Redis
	ep := svc.GetEndpoints()
	if len(ep) == 0 || ep[0].Host() != "0.0.0.0" || ep[0].Port() != 10090 {
		t.Errorf("expected '0.0.0.0:10090', got %s:%d", ep[0].Host(), ep[0].Port())
	}
}

func TestRegistry_Search_Empty(t *testing.T) {
	mock := &mockRedisClient{}
	mock.hgetAllFn = func(_ context.Context, key string) *redis.MapStringStringCmd {
		return makeMapCmd(nil)
	}

	p := patchRedis(mock)
	defer p.Reset()

	r := New(0)
	services, err := r.Search(context.Background(), gsvc.SearchInput{Name: "nonexistent"})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("expected 0 services, got %d", len(services))
	}
}

// ---- serviceFromJSON ----

func TestServiceFromJSON_Minimal(t *testing.T) {
	jsonStr := `{"Name":"friend","NodeName":"game-1@def456","Version":"","Weight":0,"NodeHost":"0.0.0.0:10091"}`

	svc, err := serviceFromJSON(jsonStr)
	if err != nil {
		t.Fatalf("serviceFromJSON failed: %v", err)
	}
	if svc.GetName() != "friend" {
		t.Errorf("expected name 'friend', got %q", svc.GetName())
	}

	ep := svc.GetEndpoints()
	if len(ep) == 0 || ep[0].Host() != "0.0.0.0" || ep[0].Port() != 10091 {
		t.Errorf("expected '0.0.0.0:10091', got %s:%d", ep[0].Host(), ep[0].Port())
	}
}

func TestServiceFromJSON_InvalidJSON(t *testing.T) {
	_, err := serviceFromJSON("{bad json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestServiceFromJSON_EmptyPodName(t *testing.T) {
	jsonStr := `{"Name":"role","NodeName":"","NodeHost":"0.0.0.0:10090"}`
	_, err := serviceFromJSON(jsonStr)
	if err == nil {
		t.Fatal("expected error for empty pod name")
	}
}
