package logic

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gogf/gf/v2/os/gcfg"
)

// testRoleLimitConfigFile 将 TOML 内容写入临时文件并构造 gcfg.Config。
func testRoleLimitConfigFile(t *testing.T, content string) *gcfg.Config {
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

// mapConfigAdapter 内存配置适配器: 允许测试构造 TOML 无法表达的 NaN/Inf 数值。
type mapConfigAdapter struct {
	data map[string]any
}

func (a *mapConfigAdapter) Available(ctx context.Context, resource ...string) bool { return true }
func (a *mapConfigAdapter) Get(ctx context.Context, pattern string) (any, error)   { return nil, nil }
func (a *mapConfigAdapter) Data(ctx context.Context) (map[string]any, error)       { return a.data, nil }

// testRoleLimitConfigMap 用内存 map 构造配置, 用于 NaN/Inf 用例。
func testRoleLimitConfigMap(t *testing.T, data map[string]any) *gcfg.Config {
	t.Helper()
	return gcfg.NewWithAdapter(&mapConfigAdapter{data: data})
}

// testRoleLimitConfig 返回所有模块启用默认策略的完整配置, 供测试构造 RoleMain。
func testRoleLimitConfig() RoleLimitConfig {
	policy := ModuleLimitPolicy{Rate: 10, Burst: 20}
	modules := make(map[string]ModuleLimitPolicy)
	for _, module := range roleModuleNames() {
		modules[module] = policy
	}
	return RoleLimitConfig{Default: policy, Modules: modules}
}

// swapRoleLimitConfig 临时替换包级限流策略并返回恢复函数, 对齐 gateway
// swapGateTokenVerifier 的测试隔离先例; 调用方应通过 t.Cleanup 恢复。
func swapRoleLimitConfig(config RoleLimitConfig) func() {
	old := roleLimitConfig
	roleLimitConfig = config
	return func() {
		roleLimitConfig = old
	}
}

func TestLoadRoleLimitConfig(t *testing.T) {
	baseDefault := "[role_limit.default]\nrate=10\nburst=20\n"
	defaultPolicy := ModuleLimitPolicy{Rate: 10, Burst: 20}

	cases := []struct {
		name    string
		content string
		want    func(t *testing.T, cfg RoleLimitConfig)
		wantErr string // 非空时断言错误包含该子串
	}{
		{
			name:    "omitted disabled defaults to false",
			content: baseDefault,
			want: func(t *testing.T, cfg RoleLimitConfig) {
				if cfg.Default != defaultPolicy {
					t.Fatalf("Default = %+v, want %+v", cfg.Default, defaultPolicy)
				}
				names := roleModuleNames()
				if len(names) != 14 {
					t.Fatalf("roleModuleNames len = %d, want 14", len(names))
				}
				if len(cfg.Modules) != len(names) {
					t.Fatalf("Modules len = %d, want %d", len(cfg.Modules), len(names))
				}
				for _, name := range names {
					pol, ok := cfg.Modules[name]
					if !ok {
						t.Fatalf("Modules missing resolved module %q", name)
					}
					if pol != defaultPolicy {
						t.Fatalf("Modules[%q] = %+v, want %+v", name, pol, defaultPolicy)
					}
				}
			},
		},
		{
			name:    "override only burst inherits rate",
			content: baseDefault + "[role_limit.modules.RoleFlower]\nburst=30\n",
			want: func(t *testing.T, cfg RoleLimitConfig) {
				wantFlower := ModuleLimitPolicy{Rate: 10, Burst: 30}
				if got := cfg.Modules["RoleFlower"]; got != wantFlower {
					t.Fatalf("Modules[RoleFlower] = %+v, want %+v", got, wantFlower)
				}
				if got := cfg.Modules["RoleMail"]; got != defaultPolicy {
					t.Fatalf("Modules[RoleMail] = %+v, want default %+v", got, defaultPolicy)
				}
			},
		},
		{
			name: "explicit false overrides disabled default",
			content: "[role_limit.default]\nrate=10\nburst=20\ndisabled=true\n" +
				"[role_limit.modules.RoleFlower]\ndisabled=false\n",
			want: func(t *testing.T, cfg RoleLimitConfig) {
				wantDefault := ModuleLimitPolicy{Rate: 10, Burst: 20, Disabled: true}
				wantFlower := ModuleLimitPolicy{Rate: 10, Burst: 20, Disabled: false}
				if cfg.Default != wantDefault {
					t.Fatalf("Default = %+v, want %+v", cfg.Default, wantDefault)
				}
				if got := cfg.Modules["RoleFlower"]; got != wantFlower {
					t.Fatalf("Modules[RoleFlower] = %+v, want %+v", got, wantFlower)
				}
			},
		},
		{
			name:    "missing default rejected",
			content: "[role_limit.modules.RoleFlower]\nburst=5\n",
			wantErr: "default",
		},
		{
			name:    "zero rate rejected",
			content: "[role_limit.default]\nrate=0\nburst=20\n",
			wantErr: "rate",
		},
		{
			name:    "negative rate rejected",
			content: "[role_limit.default]\nrate=-1\nburst=20\n",
			wantErr: "rate",
		},
		{
			name:    "zero burst rejected",
			content: "[role_limit.default]\nrate=10\nburst=0\n",
			wantErr: "burst",
		},
		{
			name:    "negative burst rejected",
			content: "[role_limit.default]\nrate=10\nburst=-1\n",
			wantErr: "burst",
		},
		{
			name: "invalid override rejected even when disabled",
			content: baseDefault +
				"[role_limit.modules.RoleFlower]\nrate=0\ndisabled=true\n",
			wantErr: "RoleFlower",
		},
		{
			name:    "unknown module rejected",
			content: baseDefault + "[role_limit.modules.RoleFlowers]\nburst=5\n",
			wantErr: "unknown role module",
		},
		{
			name:    "excluded RoleMain rejected",
			content: baseDefault + "[role_limit.modules.RoleMain]\nburst=5\n",
			wantErr: "role actor itself",
		},
		{
			name:    "unknown field rejected",
			content: baseDefault + "[role_limit.modules.RoleFlower]\nbrate=1\n",
			wantErr: "invalid keys",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testRoleLimitConfigFile(t, tc.content)
			got, err := LoadRoleLimitConfig(context.Background(), cfg)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadRoleLimitConfig failed: %v", err)
			}
			if tc.want != nil {
				tc.want(t, got)
			}
		})
	}
}

func TestLoadRoleLimitConfigRejectsNaNInfRate(t *testing.T) {
	for _, rate := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		cfg := testRoleLimitConfigMap(t, map[string]any{
			"role_limit": map[string]any{
				"default": map[string]any{"rate": rate, "burst": 20},
			},
		})
		if _, err := LoadRoleLimitConfig(context.Background(), cfg); err == nil {
			t.Fatalf("expected error for rate %v, got nil", rate)
		} else if !strings.Contains(err.Error(), "rate") {
			t.Fatalf("error = %q, want substring %q", err.Error(), "rate")
		}
	}
}
