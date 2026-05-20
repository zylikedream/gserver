package logic

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxypgx"
	"gserver/core/gxyredis"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/redis/go-redis/v9"
)

const RolePlotSnapshotCacheExpire = 24 * time.Hour

type rolePlotSnapshot struct {
	RoleID    int64     `json:"role_id"`
	Plots     PlotMap   `json:"plots"`
	UpdatedAt time.Time `json:"updated_at"`
}

type rolePlotSnapshotStore interface {
	Get(ctx context.Context, roleID int64) (PlotMap, bool)
	Set(ctx context.Context, roleID int64, plots PlotMap) error
}

var rolePlotSnapshots rolePlotSnapshotStore = redisRolePlotSnapshotStore{}

type redisRolePlotSnapshotStore struct{}

func rolePlotSnapshotKey(roleID int64) string {
	return fmt.Sprintf("role_plot_snapshot:%d", roleID)
}

func (redisRolePlotSnapshotStore) Get(ctx context.Context, roleID int64) (PlotMap, bool) {
	raw, err := gxyredis.Redis().Get(ctx, rolePlotSnapshotKey(roleID)).Result()
	if err != nil {
		if err != redis.Nil {
			gxylog.Error(ctx, "get role plot snapshot from cache failed", gxylog.Num("roleID", roleID), gxylog.Err(err))
		}
		return nil, false
	}
	snapshot := &rolePlotSnapshot{}
	if err := gjson.Unmarshal([]byte(raw), snapshot); err != nil {
		gxylog.Error(ctx, "unmarshal role plot snapshot from cache failed", gxylog.Num("roleID", roleID), gxylog.Err(err))
		return nil, false
	}
	return snapshot.Plots, true
}

func (redisRolePlotSnapshotStore) Set(ctx context.Context, roleID int64, plots PlotMap) error {
	raw, err := gjson.EncodeString(&rolePlotSnapshot{
		RoleID:    roleID,
		Plots:     clonePlotMap(plots),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("marshal role plot snapshot: %w", err)
	}
	if err := gxyredis.Redis().Set(ctx, rolePlotSnapshotKey(roleID), raw, RolePlotSnapshotCacheExpire).Err(); err != nil {
		return fmt.Errorf("set role plot snapshot: %w", err)
	}
	return nil
}

func clonePlotMap(plots PlotMap) PlotMap {
	if plots == nil {
		return nil
	}
	cloned := make(PlotMap, len(plots))
	for plotID, plot := range plots {
		if plot == nil {
			continue
		}
		copyPlot := *plot
		cloned[plotID] = &copyPlot
	}
	return cloned
}

func publishRolePlotSnapshot(ctx context.Context, roleID int64, plots PlotMap) {
	if err := rolePlotSnapshots.Set(ctx, roleID, plots); err != nil {
		gxylog.Error(ctx, "publish role plot snapshot failed", gxylog.Num("roleID", roleID), gxylog.Err(err))
	}
}

func getRolePlotSnapshot(ctx context.Context, roleID int64) (PlotMap, bool) {
	if plots, ok := rolePlotSnapshots.Get(ctx, roleID); ok {
		return plots, true
	}
	plots, ok := getRolePlotSnapshotFromDB(ctx, roleID)
	if !ok {
		return nil, false
	}
	publishRolePlotSnapshot(ctx, roleID, plots)
	return plots, true
}

func getRolePlotSnapshotFromDB(ctx context.Context, roleID int64) (PlotMap, bool) {
	var row struct {
		Plots PlotMap `gorm:"column:plots;type:jsonb"`
	}
	if err := gxypgx.DB().WithContext(ctx).Table("role_plot").
		Where("role_id = ?", roleID).First(&row).Error; err != nil {
		return nil, false
	}
	return row.Plots, true
}
