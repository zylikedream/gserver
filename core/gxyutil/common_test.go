package gxyutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gogf/gf/v2/os/gcfg"
)

func TestCfgUnmarshalUsesFileTagName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := `
[token]
expire_seconds = 300
issuer = "account-service"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	adapter, err := gcfg.NewAdapterFile(path)
	if err != nil {
		t.Fatalf("new adapter failed: %v", err)
	}
	cfg := gcfg.NewWithAdapter(adapter)

	type tokenConfig struct {
		Token struct {
			ExpireSeconds int    `toml:"expire_seconds"`
			Issuer        string `toml:"issuer"`
		} `toml:"token"`
	}

	var out tokenConfig
	if err := CfgUnmarshal(context.Background(), cfg, &out); err != nil {
		t.Fatalf("CfgUnmarshal failed: %v", err)
	}
	if out.Token.ExpireSeconds != 300 {
		t.Fatalf("ExpireSeconds = %d, want 300", out.Token.ExpireSeconds)
	}
	if out.Token.Issuer != "account-service" {
		t.Fatalf("Issuer = %q, want account-service", out.Token.Issuer)
	}
}
