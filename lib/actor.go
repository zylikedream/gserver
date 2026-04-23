package lib

import (
	"gserver/core/gxyactor"
	"strconv"
)

const (
	ROLE_ACTOR_TYPE = "role"
)

func ActivateRole(roleID int64, spawnIfNotExist ...bool) (gxyactor.PID, error) {
	pid, err := gxyactor.ActivateActor(ROLE_ACTOR_TYPE, strconv.Itoa(int(roleID)), spawnIfNotExist...)
	if err != nil {
		return nil, err
	}
	return pid, nil
}
