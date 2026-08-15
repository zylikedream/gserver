package gxypgx

import (
	"time"

	"gserver/core/gxymetrics"

	"gorm.io/gorm"
)

type metricsPlugin struct{}

func (p *metricsPlugin) Name() string { return "gxymetrics" }

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
