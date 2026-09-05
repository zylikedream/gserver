package logic

import (
	"context"
	"errors"
	"testing"

	"gserver/core/gxyactor"

	"github.com/DATA-DOG/go-sqlmock"
)

const advanceRoleActorFenceSQLPattern = `(?s)INSERT INTO role_actor_fence.*WHERE role_actor_fence\.epoch < EXCLUDED\.epoch.*role_actor_fence\.epoch = EXCLUDED\.epoch.*role_actor_fence\.node_id = EXCLUDED\.node_id`

func TestAdvanceRoleActorFenceAcceptsNewerEpoch(t *testing.T) {
	db, mock := newGormDBForRole(t)
	owner := gxyactor.ActorOwner{NodeID: "role-2@instance-b", Epoch: 42}
	mock.ExpectExec(advanceRoleActorFenceSQLPattern).
		WithArgs(int64(100), owner.NodeID, owner.Epoch, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := advanceRoleActorFence(context.Background(), db, 100, owner); err != nil {
		t.Fatalf("advanceRoleActorFence: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdvanceRoleActorFenceRejectsStaleEpoch(t *testing.T) {
	db, mock := newGormDBForRole(t)
	owner := gxyactor.ActorOwner{NodeID: "role-1@instance-a", Epoch: 41}
	mock.ExpectExec(advanceRoleActorFenceSQLPattern).
		WithArgs(int64(100), owner.NodeID, owner.Epoch, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := advanceRoleActorFence(context.Background(), db, 100, owner)
	if !errors.Is(err, errRoleActorOwnershipLost) {
		t.Fatalf("advanceRoleActorFence error = %v, want ownership lost", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLockRoleActorFenceAcceptsExactOwner(t *testing.T) {
	db, mock := newGormDBForRole(t)
	owner := gxyactor.ActorOwner{NodeID: "role-2@instance-b", Epoch: 42}
	mock.ExpectQuery(`SELECT role_id FROM role_actor_fence`).
		WithArgs(int64(100), owner.NodeID, owner.Epoch).
		WillReturnRows(sqlmock.NewRows([]string{"role_id"}).AddRow(100))

	if err := lockRoleActorFence(context.Background(), db, 100, owner); err != nil {
		t.Fatalf("lockRoleActorFence: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLockRoleActorFenceRejectsStaleOwner(t *testing.T) {
	db, mock := newGormDBForRole(t)
	owner := gxyactor.ActorOwner{NodeID: "role-1@instance-a", Epoch: 41}
	mock.ExpectQuery(`SELECT role_id FROM role_actor_fence`).
		WithArgs(int64(100), owner.NodeID, owner.Epoch).
		WillReturnRows(sqlmock.NewRows([]string{"role_id"}))

	err := lockRoleActorFence(context.Background(), db, 100, owner)
	if !errors.Is(err, errRoleActorOwnershipLost) {
		t.Fatalf("lockRoleActorFence error = %v, want ownership lost", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRoleSaveLocksFenceBeforeWritingModules(t *testing.T) {
	db, mock := newGormDBForRole(t)
	owner := gxyactor.ActorOwner{NodeID: "role-2@instance-b", Epoch: 42}
	r := newRoleMainForSave(100)
	r.deps.DB = db
	r.actorOwner = owner
	mod := &testRoleModule{state: &testPersistState{table: "test_mod"}}
	mod.state.RoleID = 100
	mod.state.MarkDirty()
	if err := r.AddModule(context.Background(), mod); err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT role_id FROM role_actor_fence`).
		WithArgs(int64(100), owner.NodeID, owner.Epoch).
		WillReturnRows(sqlmock.NewRows([]string{"role_id"}))
	mock.ExpectRollback()

	err := r.save(context.Background())
	if !errors.Is(err, errRoleActorOwnershipLost) {
		t.Fatalf("save error = %v, want ownership lost", err)
	}
	if !mod.state.IsDirty() {
		t.Fatal("rejected save cleared dirty state")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRoleModuleLocksFenceBeforeWriting(t *testing.T) {
	db, mock := newGormDBForRole(t)
	owner := gxyactor.ActorOwner{NodeID: "role-2@instance-b", Epoch: 42}
	r := newRoleMainForSave(100)
	r.deps.DB = db
	r.actorOwner = owner
	mod := &testRoleModule{state: &testPersistState{table: "test_mod"}}
	mod.state.RoleID = 100
	mod.state.MarkDirty()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT role_id FROM role_actor_fence`).
		WithArgs(int64(100), owner.NodeID, owner.Epoch).
		WillReturnRows(sqlmock.NewRows([]string{"role_id"}))
	mock.ExpectRollback()

	err := defaultSaveRoleModule(r, context.Background(), mod)
	if !errors.Is(err, errRoleActorOwnershipLost) {
		t.Fatalf("defaultSaveRoleModule error = %v, want ownership lost", err)
	}
	if !mod.state.IsDirty() {
		t.Fatal("rejected module save cleared dirty state")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
