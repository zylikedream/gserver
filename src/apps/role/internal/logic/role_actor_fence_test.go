package logic

import (
	"context"
	"errors"
	"testing"

	"gserver/core/gxyactor"

	"github.com/DATA-DOG/go-sqlmock"
)

const advanceRoleActorFenceSQLPattern = `(?s)INSERT INTO role_actor_fence.*WHERE role_actor_fence\.epoch < EXCLUDED\.epoch.*role_actor_fence\.epoch = EXCLUDED\.epoch.*role_actor_fence\.node_id = EXCLUDED\.node_id`

// TestAdvanceRoleActorFenceAcceptsNewerEpoch:抢锁时用 upsert 的 epoch 条件推进 fence 行,命中 1 行即成功。
// 为什么需要:epoch 单调递增是 invariants #1 单写者的前提——若这里放宽为无条件 upsert,
// 旧持有者(epoch 更小)仍能改回 fence,两个节点同时写同一个 role 行,后写的静默覆盖先写的,数据丢失且无任何报错。
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

// TestAdvanceRoleActorFenceRejectsStaleEpoch:RowsAffected=0(epoch 已被更新)必须返回 errRoleActorOwnershipLost。
// 为什么需要:这是 invariants #2 的拒绝方向。去掉这个判断,过期节点的抢锁会"成功",
// 于是它继续按旧快照写数据,把新 owner 的存档静默改回去,玩家资产回滚。
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

// TestLockRoleActorFenceAcceptsExactOwner:FOR UPDATE 查询命中(node_id 与 epoch 都精确匹配)才拿到行锁放行。
// 为什么需要:行锁让"校验归属"和"写模块"落在同一事务里互斥,这是 #1/#2 的执行点;
// 一旦放宽 WHERE 条件为只按 role_id 匹配,任何节点都能锁住他人正在写的 role 行。
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

// TestLockRoleActorFenceRejectsStaleOwner:FOR UPDATE 查不到行(节点/epoch 已被顶替)必须报 ownership lost。
// 为什么需要:校验必须先于任何写入。放过这个拒绝分支,旧 owner 的存档会在新 owner 已接管后落库,双写静默损坏。
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

// TestRoleSaveLocksFenceBeforeWritingModules:sqlmock 的有序期望 Begin→fence SELECT→Rollback 固定了"先校验、后写"顺序。
// 为什么需要:被保护的契约是顺序本身。若 save 先写模块再取 fence,失去 owner 的节点会先把脏数据落库(已回滚的事务只是掩盖),
// 且失败后 dirty 标记被清掉,脏状态永久丢失、内存与 DB 永久不一致。
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

// TestSaveRoleModuleLocksFenceBeforeWriting:单模块保存同样必须先取 fence 再写,拒绝时保留 dirty 标记。
// 为什么需要:与 RoleMain.save 相同的顺序契约。若先写后校验,一个非 owner 的模块保存会成功落库,
// 覆盖 owner 刚写入的新值,且调用方以为失败保留 dirty、实际已写,导致重试重复写。
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

	err := saveRoleModule(r, context.Background(), mod)
	if !errors.Is(err, errRoleActorOwnershipLost) {
		t.Fatalf("saveRoleModule error = %v, want ownership lost", err)
	}
	if !mod.state.IsDirty() {
		t.Fatal("rejected module save cleared dirty state")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
