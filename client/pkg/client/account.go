package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type PreloginRequest struct {
	Platform      string `json:"platform"`
	PlatformUID   string `json:"platform_uid"`
	ClientVersion string `json:"client_version,omitempty"`
}

type PreloginResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    *PreloginData `json:"data,omitempty"`
}

type PreloginData struct {
	AccountID   string         `json:"account_id"`
	RoleID      int64          `json:"role_id"`
	IsNewRole   bool           `json:"is_new_role"`
	AccountInfo *AccountInfo   `json:"account_info,omitempty"`
	VersionInfo *VersionInfo   `json:"version_info,omitempty"`
	Gate        *GateInfo      `json:"gate"`
	GateToken   string         `json:"gate_token"`
	ExpiresIn   int            `json:"expires_in"`
}

type AccountInfo struct {
	Platform    string `json:"platform"`
	PlatformUID string `json:"platform_uid"`
}

type VersionInfo struct {
	ClientVersion string `json:"client_version"`
	MinVersion    string `json:"min_version"`
	LatestVersion string `json:"latest_version"`
	Status        string `json:"status"`
}

type GateInfo struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

const preloginTimeout = 10 * time.Second

func AccountServerPrelogin(serverURL, platform, platformUID, clientVersion string) (*PreloginData, error) {
	reqBody := PreloginRequest{
		Platform:      platform,
		PlatformUID:   platformUID,
		ClientVersion: clientVersion,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("prelogin marshal: %w", err)
	}

	url := serverURL + "/account/prelogin"
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("prelogin new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: preloginTimeout}
	httpRsp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("prelogin request: %w", err)
	}
	defer httpRsp.Body.Close()

	rspBody, err := io.ReadAll(httpRsp.Body)
	if err != nil {
		return nil, fmt.Errorf("prelogin read body: %w", err)
	}

	var preloginRsp PreloginResponse
	if err := json.Unmarshal(rspBody, &preloginRsp); err != nil {
		return nil, fmt.Errorf("prelogin unmarshal: %w", err)
	}

	if preloginRsp.Code != 0 {
		return nil, fmt.Errorf("prelogin failed: code=%d message=%s", preloginRsp.Code, preloginRsp.Message)
	}

	if preloginRsp.Data == nil {
		return nil, fmt.Errorf("prelogin: empty data")
	}

	if preloginRsp.Data.Gate == nil {
		return nil, fmt.Errorf("prelogin: missing gate info")
	}

	return preloginRsp.Data, nil
}
