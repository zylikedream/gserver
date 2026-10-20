package gxypgx

import (
	"time"

	"gserver/core/gxymetrics"

	"gorm.io/gorm"
)

type metricsPlugin struct{}

func (p *metricsPlugin) Name() string { return "gxymetrics" }

// Initialize 注册计时回调。
//
// 覆盖六类:Query/Create/Update/Delete 与 Raw/Row。后两类不能省——gorm 把
// Exec 与 Raw(...).Scan 分别交给 Raw 与 Row 处理器,不注册就完全不在指标里;
// 归属 fencing(Exec 的 upsert、Raw 的 SELECT ... FOR UPDATE)、序列号这类关键
// 路径恰好都是裸 SQL。
func (p *metricsPlugin) Initialize(db *gorm.DB) error {
	if err := db.Callback().Query().Before("gorm:query").Register("metrics:before_query", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Query().After("gorm:query").Register("metrics:after_query", afterQuery); err != nil {
		return err
	}
	if err := db.Callback().Create().Before("gorm:create").Register("metrics:before_create", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Create().After("gorm:create").Register("metrics:after_create", afterQuery); err != nil {
		return err
	}
	if err := db.Callback().Update().Before("gorm:update").Register("metrics:before_update", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Update().After("gorm:update").Register("metrics:after_update", afterQuery); err != nil {
		return err
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register("metrics:before_delete", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Delete().After("gorm:delete").Register("metrics:after_delete", afterQuery); err != nil {
		return err
	}
	if err := db.Callback().Raw().Before("gorm:raw").Register("metrics:before_raw", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Raw().After("gorm:raw").Register("metrics:after_raw", afterQuery); err != nil {
		return err
	}
	if err := db.Callback().Row().Before("gorm:row").Register("metrics:before_row", beforeQuery); err != nil {
		return err
	}
	if err := db.Callback().Row().After("gorm:row").Register("metrics:after_row", afterQuery); err != nil {
		return err
	}
	return nil
}

func beforeQuery(db *gorm.DB) {
	db.InstanceSet("metrics:start_time", time.Now())
}

func afterQuery(db *gorm.DB) {
	start, ok := db.InstanceGet("metrics:start_time")
	if !ok {
		return
	}
	gxymetrics.DBQueryDuration.Observe(time.Since(start.(time.Time)).Seconds())
}
