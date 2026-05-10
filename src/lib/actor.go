package lib

import (
	"gserver/core/gxyactor"
	"strconv"
)

const (
	ROLE_ACTOR_TYPE = "role"
)

func GetRoleActor(roleID int64) (gxyactor.PID, error) {
	pid, err := gxyactor.ActivateActor(ROLE_ACTOR_TYPE, strconv.Itoa(int(roleID)), false)
	if err != nil {
		return nil, err
	}
	return pid, nil
}

func ActivateRole(roleID int64, spawnIfNotExist ...bool) (gxyactor.PID, error) {
	pid, err := gxyactor.ActivateActor(ROLE_ACTOR_TYPE, strconv.Itoa(int(roleID)), true)
	if err != nil {
		return nil, err
	}
	return pid, nil
}

const (
	GUILD_ACTOR_TYPE = "guild"
)

func GetGuildActor(guildID int64) (gxyactor.PID, error) {
	pid, err := gxyactor.ActivateActor(GUILD_ACTOR_TYPE, strconv.Itoa(int(guildID)), false)
	if err != nil {
		return nil, err
	}
	return pid, nil
}

const (
	CHANNEL_ACTOR_TYPE = "chat_channel"
)

// GetChannelActor 获取频道 actor，id 格式为 "channelType_int64(channelID)"
func GetChannelActor(channelType int32, channelID int64) (gxyactor.PID, error) {
	id := strconv.Itoa(int(channelType)) + "_" + strconv.FormatInt(channelID, 10)
	return gxyactor.ActivateActor(CHANNEL_ACTOR_TYPE, id, false)
}
