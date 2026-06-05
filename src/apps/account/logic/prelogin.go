package logic

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gserver/src/lib/gatetoken"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

type PreloginConfig struct {
	MinVersion    string
	LatestVersion string
	GateHost      string
	GatePort      int
	Env           string
	TokenTTL      time.Duration
	Issuer        string
}

var preloginTimeNow = time.Now

type PreloginRequest struct {
	g.Meta        `path:"/prelogin" method:"POST"`
	Platform      string `json:"platform"`
	PlatformUID   string `json:"platform_uid"`
	ClientVersion string `json:"client_version"`
}

type PreloginVersion struct {
	ClientVersion string `json:"client_version"`
	MinVersion    string `json:"min_version"`
	LatestVersion string `json:"latest_version"`
	Status        string `json:"status"`
}

type PreloginGate struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type PreloginResponse struct {
	AccountID   string          `json:"account_id"`
	RoleID      int64           `json:"role_id"`
	IsNewRole   bool            `json:"is_new_role"`
	AccountInfo map[string]any  `json:"account_info"`
	VersionInfo PreloginVersion `json:"version_info"`
	Gate        PreloginGate    `json:"gate"`
	GateToken   string          `json:"gate_token"`
	ExpiresIn   int64           `json:"expires_in"`
}

func BuildPreloginResponse(ctx context.Context, cfg PreloginConfig, signer gatetoken.Signer, platform string, platformUID string, clientVersion string) (*PreloginResponse, error) {
	if err := validateClientVersion(clientVersion, cfg.MinVersion); err != nil {
		return nil, err
	}
	account, isNewRole, err := CreateAccountWithIdentity(ctx, platform, platformUID)
	if err != nil {
		return nil, err
	}
	now := preloginTimeNow()
	token, err := signer.Sign(&gatetoken.Claims{
		AccountID: account.AccountID,
		RoleID:    account.RoleID,
		Platform:  platform,
		Env:       cfg.Env,
		IssuedAt:  now,
		ExpiresAt: now.Add(cfg.TokenTTL),
		Issuer:    cfg.Issuer,
	})
	if err != nil {
		return nil, err
	}

	return &PreloginResponse{
		AccountID: account.AccountID,
		RoleID:    account.RoleID,
		IsNewRole: isNewRole,
		AccountInfo: map[string]any{
			"platform":     platform,
			"platform_uid": platformUID,
		},
		VersionInfo: PreloginVersion{
			ClientVersion: clientVersion,
			MinVersion:    cfg.MinVersion,
			LatestVersion: cfg.LatestVersion,
			Status:        "ok",
		},
		Gate: PreloginGate{
			Host: cfg.GateHost,
			Port: cfg.GatePort,
		},
		GateToken: token,
		ExpiresIn: ttlSeconds(cfg.TokenTTL),
	}, nil
}

func ttlSeconds(ttl time.Duration) int64 {
	if ttl <= 0 {
		return 0
	}
	return int64((ttl + time.Second - 1) / time.Second)
}

func validateClientVersion(clientVersion string, minVersion string) error {
	if clientVersion == "" || minVersion == "" {
		return nil
	}
	clientParts, err := parseVersion(clientVersion)
	if err != nil {
		return err
	}
	minParts, err := parseVersion(minVersion)
	if err != nil {
		return err
	}
	for i := 0; i < max(len(clientParts), len(minParts)); i++ {
		clientValue := versionPart(clientParts, i)
		minValue := versionPart(minParts, i)
		if clientValue > minValue {
			return nil
		}
		if clientValue < minValue {
			return gerror.New("client version not supported")
		}
	}
	return nil
}

func parseVersion(version string) ([]int, error) {
	parts := strings.Split(version, ".")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, gerror.Wrapf(err, "invalid version: %s", version)
		}
		values = append(values, value)
	}
	return values, nil
}

func versionPart(values []int, index int) int {
	if index >= len(values) {
		return 0
	}
	return values[index]
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
