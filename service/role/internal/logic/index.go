package logic

import (
	"context"
	"gserver/core/gxymodule"
)

type dbIndex struct {
	gxymodule.ModuleBase
	role *RoleMain
}

func NewRoleDBIndex() *dbIndex {
	return &dbIndex{
		role: NewRoleMain(),
	}
}

func (r *dbIndex) OnModInit(ctx context.Context) error {
	r.role.initRoleModules(ctx)
	return nil
}

func (r *dbIndex) OnModStart(ctx context.Context) error {
	if err := r.ensureRoleMainIndexes(ctx); err != nil {
		return err
	}
	return nil
}

func (r *dbIndex) ensureRoleMainIndexes(ctx context.Context) error {
	for _, mod := range r.role.Modules() {
		rmod, _ := mod.(IRoleModule)
		if rmod == nil {
			continue
		}
		modState := rmod.PersistState()
		if modState == nil {
			continue
		}
		if err := ensureModIndex(ctx, modState); err != nil {
			return err
		}
	}
	return nil
}
