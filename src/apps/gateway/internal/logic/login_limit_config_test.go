// 本文件固化 login_limit 配置节的**严格解码**契约。
//
// 规则来源: login_limit_config.go 的 LoadLoginLimitConfig + validateLoginLimitConfig,
// 六个字段全部必填(rate 单位为令牌/秒,burst 为令牌容量,max_inflight 为并发登录上限,
// queue_size 为等待队列长度,wait_timeout 为排队超时);缺项、未知字段、数值非法一律报错,
// 且 **enabled=false 不豁免校验**。
// 为什么这样定: 这份配置启动时构造唯一的登录入口令牌桶,进程内没有第二道防线。
// 少校验一项,一个写错或漏写的字段会让限流静默失效(限流器形同关闭)或永久拒绝所有登录,
// 两者都不会在开发期暴露,只在压测或被撞库时以"登录接口挂了"的形式出现。
// 后果: 本文件任一用例回归 = 上述两种故障之一重新变得可配置。

package logic

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gogf/gf/v2/os/gcfg"
)

func testLoginLimitConfigFile(t *testing.T, content string) *gcfg.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gate.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter, err := gcfg.NewAdapterFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return gcfg.NewWithAdapter(adapter)
}

type loginLimitMapConfigAdapter struct {
	data map[string]any
}

func (a *loginLimitMapConfigAdapter) Available(ctx context.Context, resource ...string) bool {
	return true
}

func (a *loginLimitMapConfigAdapter) Get(ctx context.Context, pattern string) (any, error) {
	return nil, nil
}

func (a *loginLimitMapConfigAdapter) Data(ctx context.Context) (map[string]any, error) {
	return a.data, nil
}

func testLoginLimitConfigMap(t *testing.T, data map[string]any) *gcfg.Config {
	t.Helper()
	return gcfg.NewWithAdapter(&loginLimitMapConfigAdapter{data: data})
}

const validLoginLimitTOML = `[login_limit]
enabled = true
rate = 200
burst = 400
max_inflight = 100
queue_size = 500
wait_timeout = "3s"
`

const requiredLoginLimitFieldsError = "login_limit.enabled, login_limit.rate, login_limit.burst, login_limit.max_inflight, login_limit.queue_size, and login_limit.wait_timeout are required"

func disabledLoginLimitTOML(content string) string {
	return strings.Replace(content, "enabled = true", "enabled = false", 1)
}

// TestLoadLoginLimitConfig: 六个字段全缺一即报**逐字相同**的缺项错误(不是任意错误);
// 未知字段、rate/burst/max_inflight 为 0、queue_size 为负、wait_timeout 为 0 都拒绝;
// queue_size = 0 合法(不排队,直接快速失败),enabled = false 不豁免任何数值校验。
// 为什么需要: queue_size=0 与"非法 queue_size"只差一个负号,却分别是"限流"与"配置事故";
// 而 enabled=false 的分支走的是 newLoginLimiter(config, nil, ...) —— 桶根本没建,
// 数值若不校验就永远没人会发现,一旦有人改回 enabled=true 就带着非法值上线。
func TestLoadLoginLimitConfig(t *testing.T) {
	want := LoginLimitConfig{
		Enabled:     true,
		Rate:        200,
		Burst:       400,
		MaxInflight: 100,
		QueueSize:   500,
		WaitTimeout: 3 * time.Second,
	}
	cases := []struct {
		name    string
		content string
		want    LoginLimitConfig
		wantErr string
	}{
		{"valid", validLoginLimitTOML, want, ""},
		{"disabled still valid", strings.Replace(validLoginLimitTOML, "enabled = true", "enabled = false", 1), LoginLimitConfig{Rate: 200, Burst: 400, MaxInflight: 100, QueueSize: 500, WaitTimeout: 3 * time.Second}, ""},
		{"zero queue valid", strings.Replace(validLoginLimitTOML, "queue_size = 500", "queue_size = 0", 1), LoginLimitConfig{Enabled: true, Rate: 200, Burst: 400, MaxInflight: 100, WaitTimeout: 3 * time.Second}, ""},
		{"missing section", "[node]\nname='gate'\n", LoginLimitConfig{}, "login_limit"},
		{"unknown field", validLoginLimitTOML + "unknown = 1\n", LoginLimitConfig{}, "unknown"},
		{"zero rate", strings.Replace(validLoginLimitTOML, "rate = 200", "rate = 0", 1), LoginLimitConfig{}, "rate"},
		{"zero rate when disabled", disabledLoginLimitTOML(strings.Replace(validLoginLimitTOML, "rate = 200", "rate = 0", 1)), LoginLimitConfig{}, "rate"},
		{"zero burst", strings.Replace(validLoginLimitTOML, "burst = 400", "burst = 0", 1), LoginLimitConfig{}, "burst"},
		{"zero burst when disabled", disabledLoginLimitTOML(strings.Replace(validLoginLimitTOML, "burst = 400", "burst = 0", 1)), LoginLimitConfig{}, "burst"},
		{"zero max inflight", strings.Replace(validLoginLimitTOML, "max_inflight = 100", "max_inflight = 0", 1), LoginLimitConfig{}, "max_inflight"},
		{"zero max inflight when disabled", disabledLoginLimitTOML(strings.Replace(validLoginLimitTOML, "max_inflight = 100", "max_inflight = 0", 1)), LoginLimitConfig{}, "max_inflight"},
		{"negative queue", strings.Replace(validLoginLimitTOML, "queue_size = 500", "queue_size = -1", 1), LoginLimitConfig{}, "queue_size"},
		{"negative queue when disabled", disabledLoginLimitTOML(strings.Replace(validLoginLimitTOML, "queue_size = 500", "queue_size = -1", 1)), LoginLimitConfig{}, "queue_size"},
		{"zero wait timeout", strings.Replace(validLoginLimitTOML, "wait_timeout = \"3s\"", "wait_timeout = \"0s\"", 1), LoginLimitConfig{}, "wait_timeout"},
		{"zero wait timeout when disabled", disabledLoginLimitTOML(strings.Replace(validLoginLimitTOML, "wait_timeout = \"3s\"", "wait_timeout = \"0s\"", 1)), LoginLimitConfig{}, "wait_timeout"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LoadLoginLimitConfig(context.Background(), testLoginLimitConfigFile(t, tc.content))
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
				t.Fatalf("LoadLoginLimitConfig failed: %v", err)
			}
			if got != tc.want {
				t.Fatalf("LoadLoginLimitConfig = %+v, want %+v", got, tc.want)
			}
		})
	}

	missingFields := []struct {
		name string
		line string
	}{
		{"enabled", "enabled = true\n"},
		{"rate", "rate = 200\n"},
		{"burst", "burst = 400\n"},
		{"max_inflight", "max_inflight = 100\n"},
		{"queue_size", "queue_size = 500\n"},
		{"wait_timeout", "wait_timeout = \"3s\"\n"},
	}
	for _, field := range missingFields {
		t.Run("missing "+field.name, func(t *testing.T) {
			content := strings.Replace(validLoginLimitTOML, field.line, "", 1)
			_, err := LoadLoginLimitConfig(context.Background(), testLoginLimitConfigFile(t, content))
			if err == nil {
				t.Fatalf("expected error %q, got nil", requiredLoginLimitFieldsError)
			}
			if err.Error() != requiredLoginLimitFieldsError {
				t.Fatalf("error = %q, want %q", err.Error(), requiredLoginLimitFieldsError)
			}
		})
	}
}

// TestLoadLoginLimitConfigRejectsNaNInfRate: rate 为 NaN/±Inf 时必须报错,且错误文本
// 含具体的非法值(断言全文相等),以便运维从启动日志直接看出是哪个值坏了。
// 为什么需要: gxylimit.Bucket 按 rate 补令牌,NaN 让 `tokens < 1` 恒为 false ——
// acquire 每次都放行,登录限流彻底静默失效,撞库流量直达账号校验;+Inf 则让桶瞬间补满。
// TOML 表达不出这两个值,故必须走内存适配器 loginLimitMapConfigAdapter。
func TestLoadLoginLimitConfigRejectsNaNInfRate(t *testing.T) {
	cases := []struct {
		name    string
		rate    float64
		wantErr string
	}{
		{"NaN", math.NaN(), "invalid login_limit.rate NaN: must be a positive finite number"},
		{"positive infinity", math.Inf(1), "invalid login_limit.rate +Inf: must be a positive finite number"},
		{"negative infinity", math.Inf(-1), "invalid login_limit.rate -Inf: must be a positive finite number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testLoginLimitConfigMap(t, map[string]any{
				"login_limit": map[string]any{
					"enabled":      true,
					"rate":         tc.rate,
					"burst":        400,
					"max_inflight": 100,
					"queue_size":   500,
					"wait_timeout": "3s",
				},
			})
			if _, err := LoadLoginLimitConfig(context.Background(), cfg); err == nil {
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			} else if err.Error() != tc.wantErr {
				t.Fatalf("error = %q, want %q", err.Error(), tc.wantErr)
			}
		})
	}
}
