package lib

import (
	"gserver/core/gxyactor"
	"strconv"
)

const (
	ROLE_GRAIN_TYPE = "role"
)

func GetRoleGrain(roleID int64, spawnIfNotExist ...bool) (gxyactor.PID, error) {
	pid, err := gxyactor.GetGrain(ROLE_GRAIN_TYPE, strconv.Itoa(int(roleID)), spawnIfNotExist...)
	if err != nil {
		return nil, err
	}
	return pid, nil
}
