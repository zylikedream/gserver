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
func testFileConfig(t *testing.T, content string) *gcfg.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter, err := gcfg.NewAdapterFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return gcfg.NewWithAdapter(adapter)
}

func TestCfgUnmarshalKeyExactRejectsUnknownField(t *testing.T) {
	cfg := testFileConfig(t, "[role_limit.default]\nrate=10\nburst=20\nunknown=true\n")
	var out struct {
		Default struct {
			Rate  float64 `toml:"rate"`
			Burst int     `toml:"burst"`
		} `toml:"default"`
	}
	if err := CfgUnmarshalKeyExact(context.Background(), cfg, "role_limit", &out); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestCfgUnmarshalKeyExactRejectsMissingKey(t *testing.T) {
	cfg := testFileConfig(t, "[role_limit.default]\nrate=10\nburst=20\n")
	var out struct {
		Default struct {
			Rate  float64 `toml:"rate"`
			Burst int     `toml:"burst"`
		} `toml:"default"`
	}
	if err := CfgUnmarshalKeyExact(context.Background(), cfg, "missing_section", &out); err == nil {
		t.Fatal("expected missing section error")
	}
}

func TestCfgUnmarshalKeyExactDecodesSection(t *testing.T) {
	cfg := testFileConfig(t, "[role_limit.default]\nrate=10\nburst=20\n")
	var out struct {
		Default struct {
			Rate  float64 `toml:"rate"`
			Burst int     `toml:"burst"`
		} `toml:"default"`
	}
	if err := CfgUnmarshalKeyExact(context.Background(), cfg, "role_limit", &out); err != nil {
		t.Fatalf("CfgUnmarshalKeyExact failed: %v", err)
	}
	if out.Default.Rate != 10 || out.Default.Burst != 20 {
		t.Fatalf("decoded = %+v, want rate=10 burst=20", out.Default)
	}
}
