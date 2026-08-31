package logic

import (
	"testing"

	"gserver/core/gxylimit"

	"github.com/cockroachdb/errors"
)

// scriptedBucket 脚本化限流桶: 按调用顺序返回预设的 Allow 结果,
// 超出预设后一律返回 false(视为耗尽)。
type scriptedBucket struct {
	allowed []bool
	calls   int
}

func (b *scriptedBucket) Allow() bool {
	if b.calls >= len(b.allowed) {
		return false
	}
	allowed := b.allowed[b.calls]
	b.calls++
	return allowed
}

// scriptedBucketFactory 按 burst 值分发脚本化桶(工厂只能看到 gxylimit.Config,
// 测试用各模块不同的 burst 唯一标识桶)。
func scriptedBucketFactory(buckets map[int]*scriptedBucket) bucketFactory {
	return func(cfg gxylimit.Config) (tokenBucket, error) {
		b, ok := buckets[cfg.Burst]
		if !ok {
			return nil, errors.Newf("unexpected bucket config rate=%v burst=%d", cfg.Rate, cfg.Burst)
		}
		return b, nil
	}
}

func TestRoleModuleGuardDisabledDoesNotConsumeToken(t *testing.T) {
	config := testRoleLimitConfig()
	policy := config.Modules["RoleFlower"]
	policy.Disabled = true
	config.Modules["RoleFlower"] = policy
	bucket := &scriptedBucket{allowed: []bool{true}}
	guard, err := newRoleModuleGuard(config, []string{"RoleFlower"}, func(gxylimit.Config) (tokenBucket, error) {
		return bucket, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := guard.Check("RoleFlower"); got != admissionDisabled {
		t.Fatalf("got %q", got)
	}
	if bucket.calls != 0 {
		t.Fatalf("disabled bucket calls = %d", bucket.calls)
	}
}

// TestRoleModuleGuardReturnsExactResults 校验放行/耗尽/停用三种判定结果。
func TestRoleModuleGuardReturnsExactResults(t *testing.T) {
	config := testRoleLimitConfig()
	config.Modules["RoleBasic"] = ModuleLimitPolicy{Rate: 1, Burst: 1}
	config.Modules["RoleFlower"] = ModuleLimitPolicy{Rate: 2, Burst: 2}
	config.Modules["RoleMail"] = ModuleLimitPolicy{Rate: 3, Burst: 3}

	basic := &scriptedBucket{allowed: []bool{false}} // 已耗尽
	flower := &scriptedBucket{allowed: []bool{true}} // 放行
	mail := &scriptedBucket{allowed: []bool{true}}   // 放行
	guard, err := newRoleModuleGuard(config, []string{"RoleBasic", "RoleFlower", "RoleMail"},
		scriptedBucketFactory(map[int]*scriptedBucket{1: basic, 2: flower, 3: mail}))
	if err != nil {
		t.Fatal(err)
	}

	if got := guard.Check("RoleBasic"); got != admissionLimited {
		t.Fatalf("Check(RoleBasic) = %q, want limited", got)
	}
	if got := guard.Check("RoleFlower"); got != admissionOK {
		t.Fatalf("Check(RoleFlower) = %q, want ok", got)
	}
	if basic.calls != 1 {
		t.Fatalf("basic bucket calls = %d, want 1", basic.calls)
	}
	if flower.calls != 1 {
		t.Fatalf("flower bucket calls = %d, want 1", flower.calls)
	}
	if mail.calls != 0 {
		t.Fatalf("mail bucket calls = %d, want 0 (unchecked)", mail.calls)
	}
}

// TestRoleModuleGuardModuleIsolation 同一守卫内各模块桶互不影响。
func TestRoleModuleGuardModuleIsolation(t *testing.T) {
	config := testRoleLimitConfig()
	config.Modules["RoleBasic"] = ModuleLimitPolicy{Rate: 1, Burst: 1}
	config.Modules["RoleFlower"] = ModuleLimitPolicy{Rate: 2, Burst: 2}

	basic := &scriptedBucket{allowed: []bool{false}}       // 第一次即拒绝
	flower := &scriptedBucket{allowed: []bool{true, true}} // 连续放行两次
	guard, err := newRoleModuleGuard(config, []string{"RoleBasic", "RoleFlower"},
		scriptedBucketFactory(map[int]*scriptedBucket{1: basic, 2: flower}))
	if err != nil {
		t.Fatal(err)
	}

	if got := guard.Check("RoleBasic"); got != admissionLimited {
		t.Fatalf("Check(RoleBasic) = %q, want limited", got)
	}
	if got := guard.Check("RoleFlower"); got != admissionOK {
		t.Fatalf("Check(RoleFlower)[1] = %q, want ok", got)
	}
	if got := guard.Check("RoleFlower"); got != admissionOK {
		t.Fatalf("Check(RoleFlower)[2] = %q, want ok", got)
	}
	// RoleBasic 耗尽不影响 RoleFlower; 各自恰好消费自己桶的令牌。
	if basic.calls != 1 {
		t.Fatalf("basic bucket calls = %d, want 1", basic.calls)
	}
	if flower.calls != 2 {
		t.Fatalf("flower bucket calls = %d, want 2", flower.calls)
	}
}

// TestRoleModuleGuardInstanceIsolation 不同 RoleMain 实例的守卫各自持有独立桶。
func TestRoleModuleGuardInstanceIsolation(t *testing.T) {
	config := testRoleLimitConfig()
	config.Modules["RoleFlower"] = ModuleLimitPolicy{Rate: 1, Burst: 1}

	first := &scriptedBucket{allowed: []bool{true}}
	second := &scriptedBucket{allowed: []bool{false}}
	guardA, err := newRoleModuleGuard(config, []string{"RoleFlower"}, scriptedBucketFactory(map[int]*scriptedBucket{1: first}))
	if err != nil {
		t.Fatal(err)
	}
	guardB, err := newRoleModuleGuard(config, []string{"RoleFlower"}, scriptedBucketFactory(map[int]*scriptedBucket{1: second}))
	if err != nil {
		t.Fatal(err)
	}

	if got := guardA.Check("RoleFlower"); got != admissionOK {
		t.Fatalf("guardA Check = %q, want ok", got)
	}
	if got := guardB.Check("RoleFlower"); got != admissionLimited {
		t.Fatalf("guardB Check = %q, want limited", got)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("bucket calls = %d/%d, want 1/1", first.calls, second.calls)
	}
}

// TestRoleModuleGuardFactoryError 工厂失败时构造守卫必须返回错误。
func TestRoleModuleGuardFactoryError(t *testing.T) {
	config := testRoleLimitConfig()
	_, err := newRoleModuleGuard(config, []string{"RoleBasic"}, func(gxylimit.Config) (tokenBucket, error) {
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("want error from failing bucket factory")
	}
}

// TestRoleModuleGuardMissingPolicy 配置缺失的模块名视为编程错误, 构造必须失败。
func TestRoleModuleGuardMissingPolicy(t *testing.T) {
	config := testRoleLimitConfig()
	delete(config.Modules, "RoleBasic")
	_, err := newRoleModuleGuard(config, []string{"RoleBasic"}, scriptedBucketFactory(map[int]*scriptedBucket{}))
	if err == nil {
		t.Fatal("want error for module missing from resolved config")
	}
}
