package role

import (
	"testing"

	"gserver/core/gxymetrics"
	"gserver/src/apps/role/internal/logic"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestSetRoleLimitMetrics 校验启动策略到 disabled 指标的映射:
// 停用模块置 1, 启用模块置 0。
func TestSetRoleLimitMetrics(t *testing.T) {
	config := logic.RoleLimitConfig{
		Default: logic.ModuleLimitPolicy{Rate: 10, Burst: 20},
		Modules: map[string]logic.ModuleLimitPolicy{
			"RoleBasic":  {Rate: 10, Burst: 20, Disabled: true},
			"RoleFlower": {Rate: 10, Burst: 20},
		},
	}
	t.Cleanup(func() {
		gxymetrics.RoleModuleDisabled.DeleteLabelValues("RoleBasic")
		gxymetrics.RoleModuleDisabled.DeleteLabelValues("RoleFlower")
	})
	setRoleLimitMetrics(config)

	if got := testutil.ToFloat64(gxymetrics.RoleModuleDisabled.WithLabelValues("RoleBasic")); got != 1 {
		t.Fatalf("RoleModuleDisabled{RoleBasic} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(gxymetrics.RoleModuleDisabled.WithLabelValues("RoleFlower")); got != 0 {
		t.Fatalf("RoleModuleDisabled{RoleFlower} = %v, want 0", got)
	}
}
