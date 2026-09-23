package lib

import (
	"context"
	"gserver/core/gxyactor"
	"strconv"
)

const (
	ROLE_ACTOR_TYPE = "role"
)

func GetRoleActor(ctx context.Context, roleID int64) (gxyactor.PID, error) {
	pid, err := gxyactor.ActivateActor(ctx, ROLE_ACTOR_TYPE, strconv.Itoa(int(roleID)), false)
	if err != nil {
		return gxyactor.PID{}, err
	}
	return pid, nil
}

func ActivateRole(ctx context.Context, roleID int64, spawnIfNotExist ...bool) (gxyactor.PID, error) {
	pid, err := gxyactor.ActivateActor(ctx, ROLE_ACTOR_TYPE, strconv.Itoa(int(roleID)), true)
	if err != nil {
		return gxyactor.PID{}, err
	}
	return pid, nil
}

const (
	GUILD_ACTOR_TYPE = "guild"
)

func GetGuildActor(ctx context.Context, guildID int64) (gxyactor.PID, error) {
	pid, err := gxyactor.ActivateActor(ctx, GUILD_ACTOR_TYPE, strconv.Itoa(int(guildID)), true)
	if err != nil {
		return gxyactor.PID{}, err
	}
	return pid, nil
}

const (
	CHANNEL_ACTOR_TYPE = "chat_channel"
)

// GetChannelActor 获取频道 actor。id 由 ChannelKey 派生,格式 "type_id"。
func GetChannelActor(ctx context.Context, channelType int32, channelID int64) (gxyactor.PID, error) {
	return gxyactor.ActivateActor(ctx, CHANNEL_ACTOR_TYPE, (ChannelKey{Type: channelType, ID: channelID}).String(), true)
}
