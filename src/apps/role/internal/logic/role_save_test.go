package logic

import (
	"context"
	"testing"

	"gserver/core/gxymodule"
)

type testPersistState struct {
	RolePersistState
	table string
}

func (s *testPersistState) TableName() string { return s.table }

type testRoleModule struct {
	gxymodule.ModuleBase
	state *testPersistState
}

func (m *testRoleModule) SetRole(role *RoleMain)         {}
func (m *testRoleModule) OnCreate(ctx context.Context)   {}
func (m *testRoleModule) AfterLogin(ctx context.Context) {}
func (m *testRoleModule) BeforeLogout(ctx context.Context) {
}
func (m *testRoleModule) PersistState() IPersistState {
	if m.state == nil {
		return nil
	}
	return m.state
}

func TestDirtyRoleModulesOnlyReturnsDirtyPersistModules(t *testing.T) {
	ctx := context.Background()
	r := NewRoleMain()
	dirty := &testRoleModule{state: &testPersistState{table: "dirty_mod"}}
	clean := &testRoleModule{state: &testPersistState{table: "clean_mod"}}
	withoutState := &testRoleModule{}
	dirty.state.MarkDirty()

	if err := r.AddModule(ctx, dirty); err != nil {
		t.Fatal(err)
	}
	if err := r.AddModule(ctx, clean); err != nil {
		t.Fatal(err)
	}
	if err := r.AddModule(ctx, withoutState); err != nil {
		t.Fatal(err)
	}

	mods := r.dirtyRoleModules()
	if len(mods) != 1 {
		t.Fatalf("dirtyRoleModules len = %d, want 1", len(mods))
	}
	if mods[0] != dirty {
		t.Fatalf("dirtyRoleModules[0] = %T, want dirty module", mods[0])
	}
}
