package logic

type RoleExtraPersistState struct {
	RolePersistState
}

func (RoleExtraPersistState) TableName() string { return "role_extra" }

func (r *RoleExtraPersistState) GetIndexes() []string {
	return []string{"update_at"}
}

type RoleExtra struct {
	RoleModule
	RoleExtraPersistState
}

func (r *RoleExtra) PersistState() IPersistState {
	return &r.RoleExtraPersistState
}
