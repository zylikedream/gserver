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

func (s stubSigner) Verify(token string, _ time.Time) (*gatetoken.Claims, error) {
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

func (s *capturingSigner) Verify(string, time.Time) (*gatetoken.Claims, error) {
	return nil, errors.New("not implemented")
}

// TestBuildPreloginResponseRejectsOldVersion:版本低于 MinVersion 时必须先于建号与签发返回错误。
// 为什么需要:校验排在 CreateAccountWithIdentity 之前;顺序一旦颠倒,老客户端已建出账号才被拒,留下垃圾号。
func TestBuildPreloginResponseRejectsOldVersion(t *testing.T) {
	svc := newTestService(newInMemoryAccountStore())

	cfg := PreloginConfig{
		MinVersion:    "1.2.0",
		LatestVersion: "1.3.0",
		GateHost:      "gate.example.com",
		GatePort:      20001,
		Env:           "dev",
		TokenTTL:      5 * time.Minute,
		Issuer:        "account-service",
	}
	_, err := svc.BuildPreloginResponse(context.Background(), cfg, stubSigner{}, "guest", "u_2001", "1.0.0")
	if err == nil {
		t.Fatalf("expected version validation error")
	}
}

// TestBuildPreloginResponseReturnsGateAndToken:响应必须给出 gate 地址、签发串与 account_id/role_id。
// 为什么需要:客户端拿 gate_token 后要自行连网关并进入指定角色;缺字段是在握手后才暴露的失败,表现为连不上网关。
func TestBuildPreloginResponseReturnsGateAndToken(t *testing.T) {
	svc := newTestService(newInMemoryAccountStore())
	now := time.Unix(1710000000, 0)
	svc.clock = func() time.Time { return now }

	cfg := PreloginConfig{
		MinVersion:    "1.0.0",
		LatestVersion: "1.0.0",
		GateHost:      "gate.example.com",
		GatePort:      20001,
		Env:           "dev",
		TokenTTL:      5 * time.Minute,
		Issuer:        "account-service",
	}
	rsp, err := svc.BuildPreloginResponse(context.Background(), cfg, stubSigner{token: "signed-token"}, "guest", "u_2002", "1.0.0")
	if err != nil {
		t.Fatalf("build response failed: %v", err)
	}
	if rsp.Gate.Host != "gate.example.com" || rsp.Gate.Port != 20001 || rsp.GateToken != "signed-token" {
		t.Fatalf("unexpected response: %+v", rsp)
	}
	if !rsp.IsNewRole || rsp.AccountID != "acc-test-1" || rsp.RoleID != 10001 {
		t.Fatalf("unexpected identity payload: %+v", rsp)
	}
}

// TestBuildPreloginResponseRoundsPositiveTTLUpToOneSecond:ExpiresIn 对正 TTL 向上取整到秒(500ms → 1),是精度要求不是上限。
// 为什么需要:向下截断会让客户端按更短的寿命丢 token 并反复重登,或让上报寿命短于服务端实际有效期。
func TestBuildPreloginResponseRoundsPositiveTTLUpToOneSecond(t *testing.T) {
	svc := newTestService(newInMemoryAccountStore())
	now := time.Unix(1710000000, 0)
	svc.clock = func() time.Time { return now }

	cfg := PreloginConfig{
		MinVersion:    "1.0.0",
		LatestVersion: "1.0.0",
		GateHost:      "gate.example.com",
		GatePort:      20001,
		Env:           "dev",
		TokenTTL:      500 * time.Millisecond,
		Issuer:        "account-service",
	}
	rsp, err := svc.BuildPreloginResponse(context.Background(), cfg, stubSigner{token: "signed-token"}, "guest", "u_2003", "1.0.0")
	if err != nil {
		t.Fatalf("build response failed: %v", err)
	}
	if rsp.ExpiresIn != 1 {
		t.Fatalf("expected ExpiresIn=1, got %d", rsp.ExpiresIn)
	}
}

// TestBuildPreloginResponseSignsCompleteClaims:gate_token 必须带齐 account_id、role_id、platform、env、issuer、iat/exp。
// 为什么需要:网关只认 token 里的 claims;少一个 claim 客户端就无法在握手后自行路由与判断环境,失败发生在登录之后。
func TestBuildPreloginResponseSignsCompleteClaims(t *testing.T) {
	svc := newTestService(newInMemoryAccountStore())
	now := time.Unix(1710000000, 0)
	svc.clock = func() time.Time { return now }
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

	rsp, err := svc.BuildPreloginResponse(context.Background(), cfg, signer, "guest", "uid-claims", "1.3.0")
	if err != nil {
		t.Fatalf("build response: %v", err)
	}
	if signer.claims == nil {
		t.Fatal("signer did not receive claims")
	}
	if signer.claims.AccountID != "acc-test-1" ||
		signer.claims.RoleID != 10001 ||
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

// TestBuildPreloginResponsePropagatesSignerError:签名失败时返回 nil 响应并原样上浮 signer 的错误。
// 为什么需要:返回无 GateToken 的半成品响应,客户端会拿空 token 去连网关,失败点离登录很远且难排查。
func TestBuildPreloginResponsePropagatesSignerError(t *testing.T) {
	svc := newTestService(newInMemoryAccountStore())
	signErr := errors.New("signer unavailable")

	rsp, err := svc.BuildPreloginResponse(context.Background(), PreloginConfig{
		TokenTTL: time.Minute,
	}, stubSigner{err: signErr}, "guest", "uid-sign-error", "")
	if !errors.Is(err, signErr) || rsp != nil {
		t.Fatalf("expected signer error, response=%+v err=%v", rsp, err)
	}
}

// TestValidateClientVersion:版本按数字段逐位比,缺位补 0——不是字符串比较。
// 为什么需要:「equal with omitted trailing component」(1.2 vs 1.2.0)与「newer minor uses numeric comparison」(1.10.0 vs 1.2.0)两行承重:字符串比较会误拒前者、把 1.10.0 判成比 1.2.0 旧。
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

// TestTTLSeconds:正 TTL 向上取整到秒(1ns→1、1s+1ns→2),非正 TTL 归 0。
// 为什么需要:亚秒 TTL 若被截断成 0,客户端会以「token 已过期」处理刚拿到的凭据,登录后立刻失效并反复重登。
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
