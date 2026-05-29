package logic

import (
	"testing"

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
