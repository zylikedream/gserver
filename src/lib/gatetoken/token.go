package gatetoken

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
)

var timeNow = time.Now

type Claims struct {
	AccountID string    `json:"account_id"`
	RoleID    int64     `json:"role_id"`
	Platform  string    `json:"platform"`
	Env       string    `json:"env"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Issuer    string    `json:"issuer"`
}

type Signer interface {
	Sign(claims *Claims) (string, error)
	Verify(token string) (*Claims, error)
}

type Config struct {
	Algorithm     string        `toml:"algorithm"`
	Issuer        string        `toml:"issuer"`
	ExpireSeconds int           `toml:"expire_seconds"`
	Env           string        `toml:"env"`
	HS256         HS256Config   `toml:"hs256"`
	Ed25519       Ed25519Config `toml:"ed25519"`
}

type HS256Config struct {
	Secret string `toml:"secret"`
}

type Ed25519Config struct {
	PrivateKey string `toml:"private_key"`
	PublicKey  string `toml:"public_key"`
}

type hmacSigner struct {
	secret []byte
	issuer string
}

type ed25519Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	issuer     string
}

type tokenHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

func NewHMACSigner(secret string, issuer string) Signer {
	return &hmacSigner{secret: []byte(secret), issuer: issuer}
}

func NewEd25519Signer(privateKey string, publicKey string, issuer string) (Signer, error) {
	decodedPrivateKey, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return nil, gerror.Wrap(err, "decode ed25519 private key failed")
	}
	decodedPublicKey, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return nil, gerror.Wrap(err, "decode ed25519 public key failed")
	}
	if len(decodedPrivateKey) != ed25519.PrivateKeySize {
		return nil, gerror.Newf("invalid ed25519 private key length: %d", len(decodedPrivateKey))
	}
	if len(decodedPublicKey) != ed25519.PublicKeySize {
		return nil, gerror.Newf("invalid ed25519 public key length: %d", len(decodedPublicKey))
	}
	return &ed25519Signer{
		privateKey: ed25519.PrivateKey(decodedPrivateKey),
		publicKey:  ed25519.PublicKey(decodedPublicKey),
		issuer:     issuer,
	}, nil
}

func LoadSigner(cfg Config) (Signer, error) {
	switch strings.ToLower(cfg.Algorithm) {
	case "hs256":
		if cfg.HS256.Secret == "" {
			return nil, gerror.New("token.hs256.secret is required for hs256")
		}
		return NewHMACSigner(cfg.HS256.Secret, cfg.Issuer), nil
	case "ed25519":
		if cfg.Ed25519.PrivateKey == "" || cfg.Ed25519.PublicKey == "" {
			return nil, gerror.New("token.ed25519 private_key and public_key are required")
		}
		return NewEd25519Signer(cfg.Ed25519.PrivateKey, cfg.Ed25519.PublicKey, cfg.Issuer)
	default:
		return nil, gerror.Newf("unsupported token algorithm: %s", cfg.Algorithm)
	}
}

func (s *hmacSigner) Sign(claims *Claims) (string, error) {
	return signToken("HS256", claims, func(signingInput string) ([]byte, error) {
		mac := hmac.New(sha256.New, s.secret)
		if _, err := mac.Write([]byte(signingInput)); err != nil {
			return nil, err
		}
		return mac.Sum(nil), nil
	})
}

func (s *hmacSigner) Verify(token string) (*Claims, error) {
	return verifyToken(token, "HS256", s.issuer, func(signingInput string, signature []byte) bool {
		mac := hmac.New(sha256.New, s.secret)
		_, _ = mac.Write([]byte(signingInput))
		return hmac.Equal(mac.Sum(nil), signature)
	})
}

func (s *ed25519Signer) Sign(claims *Claims) (string, error) {
	return signToken("EdDSA", claims, func(signingInput string) ([]byte, error) {
		return ed25519.Sign(s.privateKey, []byte(signingInput)), nil
	})
}

func (s *ed25519Signer) Verify(token string) (*Claims, error) {
	return verifyToken(token, "EdDSA", s.issuer, func(signingInput string, signature []byte) bool {
		return ed25519.Verify(s.publicKey, []byte(signingInput), signature)
	})
}

func signToken(alg string, claims *Claims, signFn func(signingInput string) ([]byte, error)) (string, error) {
	headerData, err := json.Marshal(tokenHeader{Alg: alg, Typ: "JWT"})
	if err != nil {
		return "", gerror.Wrap(err, "marshal header failed")
	}
	payloadData, err := json.Marshal(claims)
	if err != nil {
		return "", gerror.Wrap(err, "marshal claims failed")
	}
	signingInput := encodeSegment(headerData) + "." + encodeSegment(payloadData)
	signature, err := signFn(signingInput)
	if err != nil {
		return "", gerror.Wrap(err, "sign token failed")
	}
	return signingInput + "." + encodeSegment(signature), nil
}

func verifyToken(token string, expectedAlg string, expectedIssuer string, verifyFn func(signingInput string, signature []byte) bool) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, gerror.New("invalid token format")
	}
	headerData, err := decodeSegment(parts[0])
	if err != nil {
		return nil, gerror.Wrap(err, "decode header failed")
	}
	var header tokenHeader
	if err := json.Unmarshal(headerData, &header); err != nil {
		return nil, gerror.Wrap(err, "unmarshal header failed")
	}
	if header.Alg != expectedAlg {
		return nil, gerror.Newf("unexpected token algorithm: %s", header.Alg)
	}
	payloadData, err := decodeSegment(parts[1])
	if err != nil {
		return nil, gerror.Wrap(err, "decode payload failed")
	}
	var claims Claims
	if err := json.Unmarshal(payloadData, &claims); err != nil {
		return nil, gerror.Wrap(err, "unmarshal claims failed")
	}
	signature, err := decodeSegment(parts[2])
	if err != nil {
		return nil, gerror.Wrap(err, "decode signature failed")
	}
	if !verifyFn(parts[0]+"."+parts[1], signature) {
		return nil, gerror.New("invalid token signature")
	}
	if expectedIssuer != "" && claims.Issuer != expectedIssuer {
		return nil, gerror.Newf("unexpected token issuer: %s", claims.Issuer)
	}
	if !claims.ExpiresAt.IsZero() && !claims.ExpiresAt.After(timeNow()) {
		return nil, gerror.New("token expired")
	}
	return &claims, nil
}

func encodeSegment(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeSegment(data string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(data)
}
