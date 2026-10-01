package logic

import (
	"context"
	"testing"

	"gserver/protocol/pb"
	"gserver/src/lib/gatetoken"

	"github.com/gogf/gf/v2/errors/gerror"
)

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

func TestResolveHandshakeIdentityRejectsMissingGateToken(t *testing.T) {
	d := newTestDeps()
	d.VerifyToken = func(string) (*gatetoken.Claims, error) {
		t.Fatal("verifier should not be called for empty token")
		return nil, nil
	}
	s, _, _ := newTestSession(t, d)

	if _, err := s.resolveHandshakeIdentity(""); err == nil {
		t.Fatal("expected missing gate token error")
	}
}

func TestResolveHandshakeIdentityReturnsClaims(t *testing.T) {
	d := newTestDeps()
	d.VerifyToken = func(token string) (*gatetoken.Claims, error) {
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
	}
	s, _, _ := newTestSession(t, d)

	identity, err := s.resolveHandshakeIdentity("ok-token")
	if err != nil {
		t.Fatalf("resolve handshake identity failed: %v", err)
	}
	if identity.AccountID != "acc_1" || identity.RoleID != 10001 {
		t.Fatalf("unexpected identity: %+v", identity)
	}
}

func TestResolveHandshakeIdentityRejectsExpiredToken(t *testing.T) {
	d := newTestDeps()
	d.VerifyToken = func(string) (*gatetoken.Claims, error) {
		return nil, gerror.New("token expired")
	}
	s, _, _ := newTestSession(t, d)

	if _, err := s.resolveHandshakeIdentity("expired"); err == nil {
		t.Fatal("expected expiry error")
	}
}

// TestSession_LoginAdmission_EmptyTokenSkipsAcquirer 空 token 在触碰登录准入器之前返回。
func TestSession_LoginAdmission_EmptyTokenSkipsAcquirer(t *testing.T) {
	stub := &stubLoginAcquirer{permit: noopLoginPermit{}}
	d := newTestDeps()
	d.Login = stub
	s, _, _ := newTestSession(t, d)

	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: ""}); err == nil {
		t.Fatal("expected empty token error")
	}
	if stub.calls != 0 {
		t.Fatalf("login acquirer called %d times, want 0", stub.calls)
	}
}

// TestSession_LoginAdmission_InvalidTokenSkipsAcquirer token 校验失败同样不触碰准入器。
func TestSession_LoginAdmission_InvalidTokenSkipsAcquirer(t *testing.T) {
	stub := &stubLoginAcquirer{permit: noopLoginPermit{}}
	d := newTestDeps()
	d.Login = stub
	d.VerifyToken = func(string) (*gatetoken.Claims, error) {
		return nil, gerror.New("bad token")
	}
	s, _, _ := newTestSession(t, d)

	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: "bad"}); err == nil {
		t.Fatal("expected token verification error")
	}
	if stub.calls != 0 {
		t.Fatalf("login acquirer called %d times, want 0", stub.calls)
	}
}
