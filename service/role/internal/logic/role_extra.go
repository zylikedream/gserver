package logic

import "time"

type RoleExtra struct {
	RoleModule `bson:"inline"`
	CronTm     time.Time `bson:"cron_tm"`
}

func NewRoleExtra() *RoleExtra {
	return &RoleExtra{
		CronTm: time.Now(),
	}
}

func (r *RoleExtra) GetCronTm() time.Time {
	return r.CronTm
}

func (r *RoleExtra) SetCronTm(tm time.Time) {
	r.CronTm = tm
}
