package guildlib

import (
	"context"

	"gserver/core/gxypgx"

	"github.com/cockroachdb/errors"
)

type RoleGuildRecord struct {
	RoleID  int64 `gorm:"column:role_id;primaryKey"`
	GuildID int64 `gorm:"column:guild_id"`
}

func (RoleGuildRecord) TableName() string { return "role_guild" }

func GetGuildIDByRoleID(ctx context.Context, roleID int64) (int64, error) {
	if roleID <= 0 {
		return 0, nil
	}
	state := &RoleGuildRecord{}
	err := gxypgx.DB().WithContext(ctx).Where("role_id = ?", roleID).Find(state).Error
	if err != nil {
		return 0, errors.Wrap(err, "load role guild state")
	}
	return state.GuildID, nil
}
