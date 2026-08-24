package logic

import (
	"context"
	"errors"
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

type capturingSigner struct {
	claims *gatetoken.Claims
	token  string
}

func (s *capturingSigner) Sign(claims *gatetoken.Claims) (string, error) {
	cloned := *claims
	s.claims = &cloned
	return s.token, nil
}

func (s *capturingSigner) Verify(string) (*gatetoken.Claims, error) {
	return nil, errors.New("not implemented")
}

func swapIDGenerators(t *testing.T, accountID string, roleID int64) {
	t.Helper()
	oldAccountID := generateAccountID
	oldRoleID := generateRoleID
	generateAccountID = func() (string, error) { return accountID, nil }
	generateRoleID = func() (int64, error) { return roleID, nil }
	t.Cleanup(func() {
		generateAccountID = oldAccountID
		generateRoleID = oldRoleID
	})
}

func TestCreateAccountWithIdentityCreatesFirstLogin(t *testing.T) {
	swapAccountStore(t, newInMemoryAccountStore())
	swapIDGenerators(t, "acc_1001", 10001)

	account, isNew, err := CreateAccountWithIdentity(context.Background(), "guest", "u_1001")
	if err != nil {
		t.Fatalf("create account with identity failed: %v", err)
	}
	if !isNew {
		t.Fatalf("expected new account")
	}
	if account.AccountID == "" || account.RoleID == 0 {
		t.Fatalf("unexpected account: %+v", account)
	}
}

func TestCreateAccountWithIdentityReturnsExistingRecord(t *testing.T) {
	store := newInMemoryAccountStore()
	swapAccountStore(t, store)
	swapIDGenerators(t, "acc_1002", 10002)

	first, _, err := CreateAccountWithIdentity(context.Background(), "guest", "u_1002")
	if err != nil {
		t.Fatalf("initial create failed: %v", err)
	}

	second, isNew, err := CreateAccountWithIdentity(context.Background(), "guest", "u_1002")
	if err != nil {
		t.Fatalf("second load failed: %v", err)
	}
	if isNew {
		t.Fatalf("expected existing account")
	}
	if first.AccountID != second.AccountID || first.RoleID != second.RoleID {
		t.Fatalf("account mismatch: first=%+v second=%+v", first, second)
	}
}

func TestBuildPreloginResponseRejectsOldVersion(t *testing.T) {
	swapAccountStore(t, newInMemoryAccountStore())
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
	swapAccountStore(t, newInMemoryAccountStore())
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

func TestBuildPreloginResponseRoundsPositiveTTLUpToOneSecond(t *testing.T) {
	swapAccountStore(t, newInMemoryAccountStore())
	swapIDGenerators(t, "acc_2003", 20003)
	now := time.Unix(1710000000, 0)
	preloginTimeNow = func() time.Time { return now }
	defer func() { preloginTimeNow = time.Now }()

	cfg := PreloginConfig{
		MinVersion:    "1.0.0",
		LatestVersion: "1.0.0",
		GateHost:      "gate.example.com",
		GatePort:      20001,
		Env:           "dev",
		TokenTTL:      500 * time.Millisecond,
		Issuer:        "account-service",
	}
	rsp, err := BuildPreloginResponse(context.Background(), cfg, stubSigner{token: "signed-token"}, "guest", "u_2003", "1.0.0")
	if err != nil {
		t.Fatalf("build response failed: %v", err)
	}
	if rsp.ExpiresIn != 1 {
		t.Fatalf("expected ExpiresIn=1, got %d", rsp.ExpiresIn)
	}
}

func TestAccountHandlerPreloginReturnsPayload(t *testing.T) {
	swapAccountStore(t, newInMemoryAccountStore())
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

func TestBuildPreloginResponseSignsCompleteClaims(t *testing.T) {
	swapAccountStore(t, newInMemoryAccountStore())
	swapIDGenerators(t, "acc-claims", 40001)
	now := time.Unix(1710000000, 0)
	oldTimeNow := preloginTimeNow
	preloginTimeNow = func() time.Time { return now }
	t.Cleanup(func() { preloginTimeNow = oldTimeNow })
	signer := &capturingSigner{token: "claims-token"}
	cfg := PreloginConfig{
		MinVersion:    "1.2.0",
		LatestVersion: "1.4.0",
		GateHost:      "gate.example.com",
		GatePort:      11086,
		Env:           "staging",
		TokenTTL:      90 * time.Second,
		Issuer:        "account-service",
	}

	rsp, err := BuildPreloginResponse(context.Background(), cfg, signer, "guest", "uid-claims", "1.3.0")
	if err != nil {
		t.Fatalf("build response: %v", err)
	}
	if signer.claims == nil {
		t.Fatal("signer did not receive claims")
	}
	if signer.claims.AccountID != "acc-claims" ||
		signer.claims.RoleID != 40001 ||
		signer.claims.Platform != "guest" ||
		signer.claims.Env != "staging" ||
		signer.claims.Issuer != "account-service" ||
		!signer.claims.IssuedAt.Equal(now) ||
		!signer.claims.ExpiresAt.Equal(now.Add(90*time.Second)) {
		t.Fatalf("unexpected claims: %+v", signer.claims)
	}
	if rsp.GateToken != "claims-token" ||
		rsp.ExpiresIn != 90 ||
		rsp.AccountInfo["platform"] != "guest" ||
		rsp.AccountInfo["platform_uid"] != "uid-claims" ||
		rsp.VersionInfo.ClientVersion != "1.3.0" ||
		rsp.VersionInfo.MinVersion != "1.2.0" ||
		rsp.VersionInfo.LatestVersion != "1.4.0" ||
		rsp.VersionInfo.Status != "ok" {
		t.Fatalf("unexpected response: %+v", rsp)
	}
}

func TestBuildPreloginResponsePropagatesSignerError(t *testing.T) {
	swapAccountStore(t, newInMemoryAccountStore())
	swapIDGenerators(t, "acc-sign-error", 40002)
	signErr := errors.New("signer unavailable")

	rsp, err := BuildPreloginResponse(context.Background(), PreloginConfig{
		TokenTTL: time.Minute,
	}, stubSigner{err: signErr}, "guest", "uid-sign-error", "")
	if !errors.Is(err, signErr) || rsp != nil {
		t.Fatalf("expected signer error, response=%+v err=%v", rsp, err)
	}
}

func TestValidateClientVersion(t *testing.T) {
	for _, test := range []struct {
		name          string
		clientVersion string
		minVersion    string
		wantErr       bool
	}{
		{name: "empty client version", minVersion: "1.0.0"},
		{name: "empty minimum version", clientVersion: "1.0.0"},
		{name: "equal", clientVersion: "1.2.0", minVersion: "1.2.0"},
		{name: "equal with omitted trailing component", clientVersion: "1.2", minVersion: "1.2.0"},
		{name: "newer major", clientVersion: "2.0.0", minVersion: "1.9.9"},
		{name: "newer minor uses numeric comparison", clientVersion: "1.10.0", minVersion: "1.2.0"},
		{name: "older", clientVersion: "1.1.9", minVersion: "1.2.0", wantErr: true},
		{name: "invalid client version", clientVersion: "1.beta.0", minVersion: "1.0.0", wantErr: true},
		{name: "invalid minimum version", clientVersion: "1.0.0", minVersion: "minimum", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateClientVersion(test.clientVersion, test.minVersion)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateClientVersion(%q, %q) error=%v, wantErr=%v", test.clientVersion, test.minVersion, err, test.wantErr)
			}
		})
	}
}

func TestTTLSeconds(t *testing.T) {
	for _, test := range []struct {
		ttl  time.Duration
		want int64
	}{
		{ttl: -time.Second, want: 0},
		{ttl: 0, want: 0},
		{ttl: time.Nanosecond, want: 1},
		{ttl: time.Second, want: 1},
		{ttl: time.Second + time.Nanosecond, want: 2},
	} {
		if got := ttlSeconds(test.ttl); got != test.want {
			t.Fatalf("ttlSeconds(%s)=%d, want %d", test.ttl, got, test.want)
		}
	}
}
