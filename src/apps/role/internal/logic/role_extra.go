package logic

import (
	"context"
	"time"
)

type RoleExtraPersistState struct {
	RolePersistState
	CronTm time.Time `gorm:"column:cron_tm"`
}

func (RoleExtraPersistState) TableName() string { return "role_extra" }

func (r *RoleExtraPersistState) GetIndexes() []string {
	return []string{"update_at"}
}

type RoleExtra struct {
	RoleModule
	RoleExtraPersistState
}

func (r *RoleExtra) PersistState() IPersistState {
	return &r.RoleExtraPersistState
}

func (r *RoleExtra) OnModInit(ctx context.Context) error {
	r.CronTm = time.Now()
	return nil
}

func (r *RoleExtra) GetCronTm() time.Time {
	return r.CronTm
}

func (r *RoleExtra) SetCronTm(tm time.Time) {
	r.CronTm = tm
}
