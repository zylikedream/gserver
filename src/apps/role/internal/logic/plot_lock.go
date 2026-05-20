package logic

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxylock"
	"gserver/core/gxyredis"

	"github.com/redis/go-redis/v9"
)

const (
	plotLockTTL = 3 * time.Second
)

var ErrPlotBusy = gxylock.ErrBusy

var plotLocks gxylock.Manager = gxylock.NewRedisManager(func() redis.UniversalClient {
	return gxyredis.Redis()
})

func plotLockKey(ownerID int64, plotID int32) string {
	return fmt.Sprintf("plot_lock:%d:%d", ownerID, plotID)
}

func withPlotLocks(ctx context.Context, ownerID int64, plotIDs []int32, fn func() error) error {
	keys := make([]string, 0, len(plotIDs))
	seen := make(map[int32]struct{}, len(plotIDs))
	for _, plotID := range plotIDs {
		if _, ok := seen[plotID]; ok {
			continue
		}
		seen[plotID] = struct{}{}
		keys = append(keys, plotLockKey(ownerID, plotID))
	}
	return gxylock.With(ctx, plotLocks, keys, plotLockTTL, fn)
}
