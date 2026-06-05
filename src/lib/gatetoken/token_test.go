package gatetoken

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"
)

func newTestEd25519KeyPair(t *testing.T) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key pair failed: %v", err)
	}
	return base64.StdEncoding.EncodeToString(privateKey), base64.StdEncoding.EncodeToString(publicKey)
}

func TestHMACSignerRoundTrip(t *testing.T) {
	signer := NewHMACSigner("test-secret", "account-service")
	now := time.Unix(1710000000, 0)
	timeNow = func() time.Time { return now }
	defer func() { timeNow = time.Now }()
	token, err := signer.Sign(&Claims{
		AccountID: "acc_1",
		RoleID:    10001,
		Platform:  "guest",
		Env:       "dev",
		IssuedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
		Issuer:    "account-service",
	})
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	claims, err := signer.Verify(token)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if claims.AccountID != "acc_1" || claims.RoleID != 10001 {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestEd25519SignerRejectsTamperedToken(t *testing.T) {
	privateKey, publicKey := newTestEd25519KeyPair(t)
	signer, err := NewEd25519Signer(privateKey, publicKey, "account-service")
	if err != nil {
		t.Fatalf("new signer failed: %v", err)
	}
	token, err := signer.Sign(&Claims{
		AccountID: "acc_2",
		RoleID:    10002,
		Platform:  "guest",
		Env:       "dev",
		IssuedAt:  time.Unix(1710000000, 0),
		ExpiresAt: time.Unix(1710000300, 0),
		Issuer:    "account-service",
	})
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	if _, err := signer.Verify(token + "broken"); err == nil {
		t.Fatalf("expected tampered token verification failure")
	}
}

func TestLoadSignerHS256(t *testing.T) {
	cfg := Config{
		Algorithm: "hs256",
		Issuer:    "account-service",
		HS256: HS256Config{
			Secret: "test-secret",
		},
	}
	signer, err := LoadSigner(cfg)
	if err != nil {
		t.Fatalf("load signer failed: %v", err)
	}
	if _, ok := signer.(*hmacSigner); !ok {
		t.Fatalf("expected hmacSigner, got %T", signer)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	signer := NewHMACSigner("test-secret", "account-service")
	token, err := signer.Sign(&Claims{
		AccountID: "acc_1",
		RoleID:    10001,
		Platform:  "guest",
		Env:       "dev",
		IssuedAt:  time.Unix(1710000000, 0),
		ExpiresAt: time.Unix(1710000001, 0),
		Issuer:    "account-service",
	})
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}
	timeNow = func() time.Time { return time.Unix(1710000600, 0) }
	defer func() { timeNow = time.Now }()
	if _, err := signer.Verify(token); err == nil {
		t.Fatalf("expected expiry failure")
	}
}

func TestLoadSignerConfigRequiresAlgorithmSpecificKeys(t *testing.T) {
	_, err := LoadSigner(Config{
		Algorithm: "ed25519",
		Issuer:    "account-service",
	})
	if err == nil {
		t.Fatalf("expected config error for missing Ed25519 keys")
	}
}
