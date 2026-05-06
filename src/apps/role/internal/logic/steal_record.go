package logic

import (
	"context"
	"time"

	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/os/glog"
)

type StealRecord struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	OwnerID   int64     `gorm:"column:owner_id;index:idx_owner_plot"`
	PlotID    int32     `gorm:"column:plot_id;index:idx_owner_plot"`
	StealerID int64     `gorm:"column:stealer_id;index:idx_stealer_owner"`
	FlowerID  int32     `gorm:"column:flower_id"`
	StealTime time.Time `gorm:"column:steal_time"`
}

func (StealRecord) TableName() string { return "steal_record" }

func initStealSchema(ctx context.Context) {
	_ = gxypgx.DB().AutoMigrate(&StealRecord{})
}

func countPlotStolen(ctx context.Context, ownerID int64, plotID int32) (int64, error) {
	var count int64
	err := gxypgx.DB().WithContext(ctx).Model(&StealRecord{}).
		Where("owner_id = ? AND plot_id = ?", ownerID, plotID).
		Count(&count).Error
	return count, err
}

func hasStealRecord(ctx context.Context, stealerID, ownerID int64, plotID int32) bool {
	var count int64
	err := gxypgx.DB().WithContext(ctx).Model(&StealRecord{}).
		Where("stealer_id = ? AND owner_id = ? AND plot_id = ?", stealerID, ownerID, plotID).
		Count(&count).Error
	if err != nil {
		glog.Errorf(ctx, "hasStealRecord error: %v", err)
		return true
	}
	return count > 0
}

func deletePlotStealRecords(ctx context.Context, ownerID int64, plotID int32) error {
	return gxypgx.DB().WithContext(ctx).
		Where("owner_id = ? AND plot_id = ?", ownerID, plotID).
		Delete(&StealRecord{}).Error
}
