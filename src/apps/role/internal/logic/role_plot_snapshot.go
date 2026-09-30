package logic

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxyredis"
	"gserver/src/pkg/deps"

	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/redis/go-redis/v9"

	"gorm.io/gorm"
)

const RolePlotSnapshotCacheExpire = 24 * time.Hour

type rolePlotSnapshot struct {
	RoleID    int64     `json:"role_id"`
	Plots     PlotMap   `json:"plots"`
	UpdatedAt time.Time `json:"updated_at"`
}

func rolePlotSnapshotKey(roleID int64) string {
	return fmt.Sprintf("role_plot_snapshot:%d", roleID)
}

// getRolePlotSnapshotFromCache 读缓存快照。
func getRolePlotSnapshotFromCache(ctx context.Context, cli gxyredis.Client, roleID int64) (PlotMap, bool) {
	raw, err := cli.Get(ctx, rolePlotSnapshotKey(roleID)).Result()
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

// setRolePlotSnapshot 写缓存快照(先克隆,避免调用方后续改动影响已发布内容)。
func setRolePlotSnapshot(ctx context.Context, cli gxyredis.Client, roleID int64, plots PlotMap) error {
	raw, err := gjson.EncodeString(&rolePlotSnapshot{
		RoleID:    roleID,
		Plots:     clonePlotMap(plots),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		return errors.Wrap(err, "marshal role plot snapshot")
	}
	if err := cli.Set(ctx, rolePlotSnapshotKey(roleID), raw, RolePlotSnapshotCacheExpire).Err(); err != nil {
		return errors.Wrap(err, "set role plot snapshot")
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

func publishRolePlotSnapshot(ctx context.Context, cli gxyredis.Client, roleID int64, plots PlotMap) {
	if err := setRolePlotSnapshot(ctx, cli, roleID, plots); err != nil {
		gxylog.Error(ctx, "publish role plot snapshot failed", gxylog.Num("roleID", roleID), gxylog.Err(err))
	}
}

func getRolePlotSnapshot(ctx context.Context, d deps.Deps, roleID int64) (PlotMap, bool) {
	if plots, ok := getRolePlotSnapshotFromCache(ctx, d.Redis, roleID); ok {
		return plots, true
	}
	plots, ok := getRolePlotSnapshotFromDB(ctx, d.DB, roleID)
	if !ok {
		return nil, false
	}
	publishRolePlotSnapshot(ctx, d.Redis, roleID, plots)
	return plots, true
}

func getRolePlotSnapshotFromDB(ctx context.Context, db *gorm.DB, roleID int64) (PlotMap, bool) {
	var row struct {
		Plots PlotMap `gorm:"column:plots;type:jsonb"`
	}
	if err := db.WithContext(ctx).Table("role_plot").
		Where("role_id = ?", roleID).First(&row).Error; err != nil {
		return nil, false
	}
	return row.Plots, true
}
