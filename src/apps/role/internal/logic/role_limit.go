package logic

import (
	"gserver/core/gxylimit"

	"github.com/cockroachdb/errors"
)

// moduleAdmission 是单个角色业务模块的准入判定结果, 同时用作指标的固定 result 标签。
type moduleAdmission string

const (
	// admissionOK 表示放行, 恰好消费一个令牌。
	admissionOK moduleAdmission = "ok"
	// admissionLimited 表示模块请求预算耗尽, 拒绝请求。
	admissionLimited moduleAdmission = "limited"
	// admissionDisabled 表示模块被启动策略停用, 拒绝请求(不消费令牌)。
	admissionDisabled moduleAdmission = "disabled"
)

// tokenBucket 抽象限流桶, 生产实现为 gxylimit.Bucket。
type tokenBucket interface {
	Allow() bool
}

// bucketFactory 按策略配置创建限流桶; 测试可注入脚本化实现。
type bucketFactory func(gxylimit.Config) (tokenBucket, error)

// newLimitBucket 是生产适配器: gxylimit.NewBucket 返回具体 *Bucket,
// 而工厂需要返回 tokenBucket 接口, Go 函数返回类型不可协变, 故需薄适配层。
func newLimitBucket(config gxylimit.Config) (tokenBucket, error) {
	return gxylimit.NewBucket(config)
}

// roleModuleGuard 持有注册了客户端 Handler 的每个角色模块的限流桶与已解析策略。
// 每个 RoleMain 实例独立构建, 同一进程内不同玩家互不影响。
type roleModuleGuard struct {
	buckets  map[string]tokenBucket
	policies map[string]ModuleLimitPolicy
}

// newRoleModuleGuard 仅为持有客户端 Handler 的模块创建桶; disabled 模块不创建桶,
// Check 直接返回 admissionDisabled, 不消费令牌。
func newRoleModuleGuard(config RoleLimitConfig, moduleNames []string, factory bucketFactory) (*roleModuleGuard, error) {
	buckets := make(map[string]tokenBucket, len(moduleNames))
	policies := make(map[string]ModuleLimitPolicy, len(moduleNames))
	for _, name := range moduleNames {
		policy, ok := config.Modules[name]
		if !ok {
			return nil, errors.Newf("role module %q missing from resolved limit config", name)
		}
		policies[name] = policy
		if policy.Disabled {
			continue
		}
		bucket, err := factory(gxylimit.Config{Rate: policy.Rate, Burst: policy.Burst})
		if err != nil {
			return nil, errors.Wrapf(err, "create bucket for role module %q", name)
		}
		buckets[name] = bucket
	}
	return &roleModuleGuard{buckets: buckets, policies: policies}, nil
}

// Check 返回模块准入判定: disabled 优先于令牌消费, 放行时恰好消费一个令牌。
func (g *roleModuleGuard) Check(module string) moduleAdmission {
	policy, ok := g.policies[module]
	if !ok {
		// 防御分支: 未纳入守卫的模块不设防(moduleByMessage 与守卫同源, 正常不会触发)。
		return admissionOK
	}
	if policy.Disabled {
		return admissionDisabled
	}
	bucket, ok := g.buckets[module]
	if !ok {
		return admissionOK
	}
	if !bucket.Allow() {
		return admissionLimited
	}
	return admissionOK
}
