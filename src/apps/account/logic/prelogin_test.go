package logic

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gserver/src/lib/gatetoken"
)

type stubSigner struct {
	token  string
	claims *gatetoken.Claims
	err    error
}

func (s stubSigner) Sign(claims *gatetoken.Claims) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.token != "" {
		return s.token, nil
	}
	return fmt.Sprintf("token-for-%s", claims.AccountID), nil
}

func (s stubSigner) Verify(token string) (*gatetoken.Claims, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.claims, nil
}

type inMemoryAccountMappingStore struct {
	byPlatformUID map[string]*AccountMapping
}

func newInMemoryAccountMappingStore() *inMemoryAccountMappingStore {
	return &inMemoryAccountMappingStore{
		byPlatformUID: make(map[string]*AccountMapping),
	}
}

func (s *inMemoryAccountMappingStore) FindByPlatformUID(_ context.Context, platform string, platformUID string) (*AccountMapping, error) {
	mapping, ok := s.byPlatformUID[platform+":"+platformUID]
	if !ok {
		return nil, nil
	}
	cloned := *mapping
	return &cloned, nil
}

func (s *inMemoryAccountMappingStore) Create(_ context.Context, mapping *AccountMapping) error {
	key := mapping.Platform + ":" + mapping.PlatformUID
	cloned := *mapping
	s.byPlatformUID[key] = &cloned
	return nil
}

func swapAccountMappingStore(t *testing.T, store accountMappingStore) {
	t.Helper()
	oldStore := accountMappings
	accountMappings = store
	t.Cleanup(func() {
		accountMappings = oldStore
	})
}

func swapIDGenerators(t *testing.T, accountID string, roleID int64) {
	t.Helper()
	oldAccountID := generateAccountID
	oldRoleID := generateRoleID
	oldEnsureLegacy := ensureLegacyRoleAccount
	generateAccountID = func() (string, error) { return accountID, nil }
	generateRoleID = func() (int64, error) { return roleID, nil }
	ensureLegacyRoleAccount = func(_ context.Context, _ *AccountMapping) error { return nil }
	t.Cleanup(func() {
		generateAccountID = oldAccountID
		generateRoleID = oldRoleID
		ensureLegacyRoleAccount = oldEnsureLegacy
	})
}

func TestLoadOrCreateAccountMappingCreatesFirstLogin(t *testing.T) {
	swapAccountMappingStore(t, newInMemoryAccountMappingStore())
	swapIDGenerators(t, "acc_1001", 10001)

	mapping, isNew, err := LoadOrCreateAccountMapping(context.Background(), "guest", "u_1001")
	if err != nil {
		t.Fatalf("load or create failed: %v", err)
	}
	if !isNew {
		t.Fatalf("expected new mapping")
	}
	if mapping.AccountID == "" || mapping.RoleID == 0 {
		t.Fatalf("unexpected mapping: %+v", mapping)
	}
}

func TestLoadOrCreateAccountMappingReturnsExistingRecord(t *testing.T) {
	store := newInMemoryAccountMappingStore()
	swapAccountMappingStore(t, store)
	swapIDGenerators(t, "acc_1002", 10002)

	first, _, err := LoadOrCreateAccountMapping(context.Background(), "guest", "u_1002")
	if err != nil {
		t.Fatalf("initial create failed: %v", err)
	}

	second, isNew, err := LoadOrCreateAccountMapping(context.Background(), "guest", "u_1002")
	if err != nil {
		t.Fatalf("second load failed: %v", err)
	}
	if isNew {
		t.Fatalf("expected existing mapping")
	}
	if first.AccountID != second.AccountID || first.RoleID != second.RoleID {
		t.Fatalf("mapping mismatch: first=%+v second=%+v", first, second)
	}
}

func TestBuildPreloginResponseRejectsOldVersion(t *testing.T) {
	swapAccountMappingStore(t, newInMemoryAccountMappingStore())
	swapIDGenerators(t, "acc_2001", 20001)

	cfg := PreloginConfig{
		MinVersion:    "1.2.0",
		LatestVersion: "1.3.0",
		GateHost:      "gate.example.com",
		GatePort:      20001,
		Env:           "dev",
		TokenTTL:      5 * time.Minute,
		Issuer:        "account-service",
	}
	_, err := BuildPreloginResponse(context.Background(), cfg, stubSigner{}, "guest", "u_2001", "1.0.0")
	if err == nil {
		t.Fatalf("expected version validation error")
	}
}

func TestBuildPreloginResponseReturnsGateAndToken(t *testing.T) {
	swapAccountMappingStore(t, newInMemoryAccountMappingStore())
	swapIDGenerators(t, "acc_2002", 20002)
	now := time.Unix(1710000000, 0)
	preloginTimeNow = func() time.Time { return now }
	defer func() { preloginTimeNow = time.Now }()

	cfg := PreloginConfig{
		MinVersion:    "1.0.0",
		LatestVersion: "1.0.0",
		GateHost:      "gate.example.com",
		GatePort:      20001,
		Env:           "dev",
		TokenTTL:      5 * time.Minute,
		Issuer:        "account-service",
	}
	rsp, err := BuildPreloginResponse(context.Background(), cfg, stubSigner{token: "signed-token"}, "guest", "u_2002", "1.0.0")
	if err != nil {
		t.Fatalf("build response failed: %v", err)
	}
	if rsp.Gate.Host != "gate.example.com" || rsp.Gate.Port != 20001 || rsp.GateToken != "signed-token" {
		t.Fatalf("unexpected response: %+v", rsp)
	}
	if !rsp.IsNewRole || rsp.AccountID != "acc_2002" || rsp.RoleID != 20002 {
		t.Fatalf("unexpected identity payload: %+v", rsp)
	}
}

func TestAccountHandlerPreloginReturnsPayload(t *testing.T) {
	swapAccountMappingStore(t, newInMemoryAccountMappingStore())
	swapIDGenerators(t, "acc_3001", 30001)
	now := time.Unix(1710000000, 0)
	preloginTimeNow = func() time.Time { return now }
	defer func() { preloginTimeNow = time.Now }()

	handler := &AccountHandler{
		Config: PreloginConfig{
			MinVersion:    "1.0.0",
			LatestVersion: "1.0.0",
			GateHost:      "gate.example.com",
			GatePort:      20001,
			Env:           "dev",
			TokenTTL:      5 * time.Minute,
			Issuer:        "account-service",
		},
		Signer: stubSigner{token: "signed-token"},
	}

	resp, err := handler.Prelogin(context.Background(), &PreloginRequest{
		Platform:      "guest",
		PlatformUID:   "u_3001",
		ClientVersion: "1.0.0",
	})
	if err != nil {
		t.Fatalf("prelogin handler returned error: %v", err)
	}
	payload, ok := resp.(*PreloginResponse)
	if !ok {
		t.Fatalf("unexpected handler response type: %T", resp)
	}
	if payload.AccountID != "acc_3001" || payload.GateToken != "signed-token" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}
