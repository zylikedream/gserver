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

type rolePlotSnapshotStore interface {
	Get(ctx context.Context, roleID int64) (PlotMap, bool)
	Set(ctx context.Context, roleID int64, plots PlotMap) error
}

// rolePlotSnapshots 由组装根初始化;测试可替换为自定义 store。
var rolePlotSnapshots rolePlotSnapshotStore = redisRolePlotSnapshotStore{redis: gxyredis.Redis}

type redisRolePlotSnapshotStore struct {
	// redis 惰性获取:包级初始化不触碰全局,方法调用时才取。
	redis func() gxyredis.Client
}

func rolePlotSnapshotKey(roleID int64) string {
	return fmt.Sprintf("role_plot_snapshot:%d", roleID)
}

func (s redisRolePlotSnapshotStore) Get(ctx context.Context, roleID int64) (PlotMap, bool) {
	raw, err := s.redis().Get(ctx, rolePlotSnapshotKey(roleID)).Result()
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

func (s redisRolePlotSnapshotStore) Set(ctx context.Context, roleID int64, plots PlotMap) error {
	raw, err := gjson.EncodeString(&rolePlotSnapshot{
		RoleID:    roleID,
		Plots:     clonePlotMap(plots),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		return errors.Wrap(err, "marshal role plot snapshot")
	}
	if err := s.redis().Set(ctx, rolePlotSnapshotKey(roleID), raw, RolePlotSnapshotCacheExpire).Err(); err != nil {
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

func publishRolePlotSnapshot(ctx context.Context, roleID int64, plots PlotMap) {
	if err := rolePlotSnapshots.Set(ctx, roleID, plots); err != nil {
		gxylog.Error(ctx, "publish role plot snapshot failed", gxylog.Num("roleID", roleID), gxylog.Err(err))
	}
}

func getRolePlotSnapshot(ctx context.Context, d deps.Deps, roleID int64) (PlotMap, bool) {
	if plots, ok := rolePlotSnapshots.Get(ctx, roleID); ok {
		return plots, true
	}
	plots, ok := getRolePlotSnapshotFromDB(ctx, d.DB, roleID)
	if !ok {
		return nil, false
	}
	publishRolePlotSnapshot(ctx, roleID, plots)
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
