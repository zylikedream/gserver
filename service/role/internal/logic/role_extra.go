package logic

import (
	"context"
	"time"
)

type RoleExtraPersistState struct {
	RolePersistState `bson:"inline"`
	CronTm           time.Time `bson:"cron_tm"`
}

type RoleExtra struct {
	RoleModule `bson:"inline"`
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
