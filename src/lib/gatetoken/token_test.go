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

// TestHMACSignerRoundTrip:HS256 签出的 token 必须在同 issuer 下验回完整 claims(签名的序列化形式本身即契约)。
// 为什么需要:header.payload 两段 base64 拼接或签名输入改一处,签发端与验签端输入就不一致,登录全线失败。
func TestHMACSignerRoundTrip(t *testing.T) {
	now := time.Unix(1710000000, 0)
	signer := NewHMACSigner("test-secret", "account-service")
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
	claims, err := signer.Verify(token, now)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if claims.AccountID != "acc_1" || claims.RoleID != 10001 {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

// TestEd25519SignerRejectsTamperedToken:token 任一段被改动都必须验签失败。
// 为什么需要:gate_token 是进网关的唯一凭据;篡改角色 id 或有效期仍能过验签,等于任意角色任意登录。
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
	if _, err := signer.Verify(token+"broken", time.Unix(1710000001, 0)); err == nil {
		t.Fatalf("expected tampered token verification failure")
	}
}

// TestLoadSignerHS256:配置 algorithm=hs256 时 LoadSigner 必须落到 HMAC 实现并带上 issuer。
// 为什么需要:算法分派写错(落到 default 或另一分支)会用错误的密钥体系签发,所有 token 验不过或被降级。
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

// TestVerifyRejectsExpiredToken:ExpiresAt 已过的 token 必须拒绝。
// 为什么需要:放过过期 token 等于凭据无期限有效——被截获的 gate_token 登出、踢下线、换设备全都不生效。
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
	if _, err := signer.Verify(token, time.Unix(1710000600, 0)); err == nil {
		t.Fatalf("expected expiry failure")
	}
}

// TestLoadSignerConfigRequiresAlgorithmSpecificKeys:ed25519 配置缺任一密钥必须加载失败,不得构造出签名器。
// 为什么需要:一旦放行,签发方会拿着空/弱密钥运行,攻击者用自己的密钥对任意 claims 签名即可伪造 gate_token。
func TestLoadSignerConfigRequiresAlgorithmSpecificKeys(t *testing.T) {
	_, err := LoadSigner(Config{
		Algorithm: "ed25519",
		Issuer:    "account-service",
	})
	if err == nil {
		t.Fatalf("expected config error for missing Ed25519 keys")
	}
}
