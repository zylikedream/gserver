package logic

import (
	"context"
	"testing"

	"gserver/protocol/pb"
	"gserver/src/lib/gatetoken"

	"github.com/gogf/gf/v2/errors/gerror"
)

func TestResolveHandshakeIdentityRejectsMissingGateToken(t *testing.T) {
	restore := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
		t.Fatalf("verifier should not be called for empty token")
		return nil, nil
	})
	defer restore()

	if _, err := resolveHandshakeIdentity(""); err == nil {
		t.Fatalf("expected missing gate token error")
	}
}

func TestResolveHandshakeIdentityReturnsClaims(t *testing.T) {
	restore := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
		if token != "ok-token" {
			t.Fatalf("unexpected token: %s", token)
		}
		return &gatetoken.Claims{
			AccountID: "acc_1",
			RoleID:    10001,
			Platform:  "guest",
			Env:       "dev",
			Issuer:    "account-service",
		}, nil
	})
	defer restore()

	identity, err := resolveHandshakeIdentity("ok-token")
	if err != nil {
		t.Fatalf("resolve handshake identity failed: %v", err)
	}
	if identity.AccountID != "acc_1" || identity.RoleID != 10001 {
		t.Fatalf("unexpected identity: %+v", identity)
	}
}

func TestResolveHandshakeIdentityRejectsExpiredToken(t *testing.T) {
	restore := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
		return nil, gerror.New("token expired")
	})
	defer restore()

	if _, err := resolveHandshakeIdentity("expired"); err == nil {
		t.Fatalf("expected expiry error")
	}
}

func swapGateTokenVerifier(fn func(token string) (*gatetoken.Claims, error)) func() {
	old := verifyGateToken
	verifyGateToken = fn
	return func() {
		verifyGateToken = old
	}
}

// stubLoginAcquirer 脚本化登录准入器:记录调用次数,返回预设 permit/err。
type stubLoginAcquirer struct {
	permit loginPermit
	err    error
	calls  int
}

func (a *stubLoginAcquirer) acquire(context.Context) (loginPermit, error) {
	a.calls++
	return a.permit, a.err
}

// recordingLoginPermit 记录 Release 调用次数。
type recordingLoginPermit struct {
	releases int
}

func (p *recordingLoginPermit) Release() {
	p.releases++
}

// TestSession_LoginAdmission_EmptyTokenSkipsAcquirer 空 token 在触碰登录准入器之前返回。
func TestSession_LoginAdmission_EmptyTokenSkipsAcquirer(t *testing.T) {
	s, _, _ := newTestSession(t)
	stub := &stubLoginAcquirer{permit: noopLoginPermit{}}
	restore := swapLoginAcquirer(stub)
	defer restore()

	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: ""}); err == nil {
		t.Fatal("expected empty token error")
	}
	if stub.calls != 0 {
		t.Fatalf("login acquirer called %d times, want 0", stub.calls)
	}
}

// TestSession_LoginAdmission_InvalidTokenSkipsAcquirer token 校验失败同样不触碰准入器。
func TestSession_LoginAdmission_InvalidTokenSkipsAcquirer(t *testing.T) {
	s, _, _ := newTestSession(t)
	stub := &stubLoginAcquirer{permit: noopLoginPermit{}}
	restore := swapLoginAcquirer(stub)
	defer restore()
	restoreToken := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
		return nil, gerror.New("bad token")
	})
	defer restoreToken()

	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: "bad"}); err == nil {
		t.Fatal("expected token verification error")
	}
	if stub.calls != 0 {
		t.Fatalf("login acquirer called %d times, want 0", stub.calls)
	}
}
