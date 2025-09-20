package logic

import (
	"fmt"

	"ergo.services/ergo/act"
)

type RoleMain struct {
	act.Actor
	RoleID  uint64
	Account string

	modsHash map[string]uint64
}

func NewRoleMain() *RoleMain {
	return &RoleMain{}
}

func (role *RoleMain) Init(args ...any) error {
	account, ok := args[0].(string)
	if !ok {
		return fmt.Errorf("args[0] is not string")
	}
	role.modsHash = make(map[string]uint64)
	role.Account = account
	return nil
}
