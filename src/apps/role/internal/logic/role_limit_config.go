package logic

import (
	"context"
	"math"
	"reflect"
	"sort"
	"strings"

	"gserver/core/gxyutil"

	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/os/gcfg"
)

// ModuleLimitPolicy 是单个角色模块的限流策略。合法策略要求 rate 为正的有限数,
// burst 为正整数; disabled 表示模块降级, 请求将被拒绝(ACK_CODE_MODULE_DISABLED),
// 但策略本身仍必须合法。
type ModuleLimitPolicy struct {
	Rate     float64
	Burst    int
	Disabled bool
}

// RoleLimitConfig 是启动时一次性解析的不可变角色限流配置。
// Modules 对每个合法 roleModules 类型名都已解析完毕, 消费方无需再做继承。
type RoleLimitConfig struct {
	Default ModuleLimitPolicy
	Modules map[string]ModuleLimitPolicy
}

// roleLimitConfig 是组装根在启动时注入的限流策略(ADR-0001 可替换变量模式,
// 与 gateway verifyGateToken 的 SetGateTokenVerifier 先例一致)。
// 仅启动阶段写入, 运行期只读; 测试在同包直接赋值覆盖。
var roleLimitConfig RoleLimitConfig

// SetRoleLimitConfig 由 roleApp.OnModInit 在 Actor kind 注册前调用,
// 严格校验通过后注入包级不可变策略。
func SetRoleLimitConfig(config RoleLimitConfig) {
	roleLimitConfig = config
}

// rawModuleLimitPolicy 使用指针字段区分"未配置"与"显式零值/显式 false"。
type rawModuleLimitPolicy struct {
	Rate     *float64 `toml:"rate"`
	Burst    *int     `toml:"burst"`
	Disabled *bool    `toml:"disabled"`
}

type rawRoleLimitConfig struct {
	Default rawModuleLimitPolicy            `toml:"default"`
	Modules map[string]rawModuleLimitPolicy `toml:"modules"`
}

// roleModuleNames 返回全部合法 roleModules 类型名(字典序), 即 roleModules 结构体
// 中指针字段指向的元素类型名。RoleMain 是角色 Actor 本身而非模块, 不在集合内。
// Task 5 的限流守卫同样以该集合为唯一合法模块来源, 请勿改动其语义。
func roleModuleNames() []string {
	t := reflect.TypeFor[roleModules]()
	names := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Type.Kind() == reflect.Pointer {
			names = append(names, field.Type.Elem().Name())
		}
	}
	sort.Strings(names)
	return names
}

// roleModuleNameSet 返回小写模块名 → 规范类型名 的映射。
// viper 会把配置 key 统一小写, 因此模块名必须做大小写无关匹配。
func roleModuleNameSet() map[string]string {
	set := make(map[string]string, len(roleModuleNames()))
	for _, name := range roleModuleNames() {
		set[strings.ToLower(name)] = name
	}
	return set
}

// resolveModuleLimitPolicy 按继承规则解析单个策略: 先取默认值, 再覆盖显式字段,
// 随后校验有效 rate/burst 为正的有限值。即使 disabled 也必须通过数值校验。
func resolveModuleLimitPolicy(raw *rawModuleLimitPolicy, def *ModuleLimitPolicy) (ModuleLimitPolicy, error) {
	policy := ModuleLimitPolicy{}
	if def != nil {
		policy = *def
	}
	if raw.Rate != nil {
		policy.Rate = *raw.Rate
	}
	if raw.Burst != nil {
		policy.Burst = *raw.Burst
	}
	if raw.Disabled != nil {
		policy.Disabled = *raw.Disabled
	}
	if math.IsNaN(policy.Rate) || math.IsInf(policy.Rate, 0) || policy.Rate <= 0 {
		return ModuleLimitPolicy{}, errors.Newf("invalid rate %v: must be a positive finite number", policy.Rate)
	}
	if policy.Burst <= 0 {
		return ModuleLimitPolicy{}, errors.Newf("invalid burst %d: must be a positive integer", policy.Burst)
	}
	return policy, nil
}

// LoadRoleLimitConfig 严格解码 role_limit 配置节并解析出完整的不变配置:
//   - 未知字段、缺失 role_limit 节、缺失 default.rate/burst 一律报错;
//   - 模块覆盖只允许 roleModules 集合内的类型名(RoleMain 与集合外名字均拒绝);
//   - 返回的 Modules 包含全部 14 个合法模块, 未显式配置的模块继承 default。
func LoadRoleLimitConfig(ctx context.Context, cfg *gcfg.Config) (RoleLimitConfig, error) {
	var raw rawRoleLimitConfig
	if err := gxyutil.CfgUnmarshalKeyExact(ctx, cfg, "role_limit", &raw); err != nil {
		return RoleLimitConfig{}, errors.Wrap(err, "decode role_limit")
	}
	if raw.Default.Rate == nil || raw.Default.Burst == nil {
		return RoleLimitConfig{}, errors.New("role_limit.default.rate and role_limit.default.burst are required")
	}
	defaultPolicy, err := resolveModuleLimitPolicy(&raw.Default, nil)
	if err != nil {
		return RoleLimitConfig{}, errors.Wrap(err, "invalid role_limit.default")
	}
	nameSet := roleModuleNameSet()
	modules := make(map[string]ModuleLimitPolicy, len(nameSet))
	for rawName, rawPolicy := range raw.Modules {
		if strings.EqualFold(rawName, "RoleMain") {
			return RoleLimitConfig{}, errors.Newf("role module %q is the role actor itself, not a module", rawName)
		}
		canonical, ok := nameSet[strings.ToLower(rawName)]
		if !ok {
			return RoleLimitConfig{}, errors.Newf("unknown role module %q", rawName)
		}
		policy, err := resolveModuleLimitPolicy(&rawPolicy, &defaultPolicy)
		if err != nil {
			return RoleLimitConfig{}, errors.Wrapf(err, "invalid role module %q", canonical)
		}
		modules[canonical] = policy
	}
	for _, name := range roleModuleNames() {
		if _, ok := modules[name]; !ok {
			modules[name] = defaultPolicy
		}
	}
	return RoleLimitConfig{Default: defaultPolicy, Modules: modules}, nil
}
