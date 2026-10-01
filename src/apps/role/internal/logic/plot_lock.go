package logic

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxylock"

	"github.com/redis/go-redis/v9"
)

const (
	plotLockTTL = 3 * time.Second
)

var ErrPlotBusy = gxylock.ErrBusy

func plotLockKey(ownerID int64, plotID int32) string {
	return fmt.Sprintf("plot_lock:%d:%d", ownerID, plotID)
}

// withPlotLocks 在若干地块锁内执行 fn。锁管理器只是参数容器,按调用构造;
// Redis 从接收者取(deps 注入,测试用 miniredis 跑同一套加锁代码)。
func (r *RoleModule) withPlotLocks(ctx context.Context, ownerID int64, plotIDs []int32, fn func() error) error {
	keys := make([]string, 0, len(plotIDs))
	seen := make(map[int32]struct{}, len(plotIDs))
	for _, plotID := range plotIDs {
		if _, ok := seen[plotID]; ok {
			continue
		}
		seen[plotID] = struct{}{}
		keys = append(keys, plotLockKey(ownerID, plotID))
	}
	manager := gxylock.NewRedisManager(func() redis.UniversalClient { return r.Redis() })
	return gxylock.With(ctx, manager, keys, plotLockTTL, fn)
}
