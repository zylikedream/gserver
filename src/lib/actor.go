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
		return nil, err
	}
	return pid, nil
}

func ActivateRole(ctx context.Context, roleID int64, spawnIfNotExist ...bool) (gxyactor.PID, error) {
	pid, err := gxyactor.ActivateActor(ctx, ROLE_ACTOR_TYPE, strconv.Itoa(int(roleID)), true)
	if err != nil {
		return nil, err
	}
	return pid, nil
}

const (
	GUILD_ACTOR_TYPE = "guild"
)

func GetGuildActor(ctx context.Context, guildID int64) (gxyactor.PID, error) {
	pid, err := gxyactor.ActivateActor(ctx, GUILD_ACTOR_TYPE, strconv.Itoa(int(guildID)), true)
	if err != nil {
		return nil, err
	}
	return pid, nil
}

const (
	CHANNEL_ACTOR_TYPE = "chat_channel"
)

// GetChannelActor 获取频道 actor，id 格式为 "channelType_int64(channelID)"
func GetChannelActor(ctx context.Context, channelType int32, channelID int64) (gxyactor.PID, error) {
	id := strconv.Itoa(int(channelType)) + "_" + strconv.FormatInt(channelID, 10)
	return gxyactor.ActivateActor(ctx, CHANNEL_ACTOR_TYPE, id, true)
}
