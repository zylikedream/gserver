package logic

import (
	"context"
	"testing"
	"time"

	"gserver/protocol/pb"
	"gserver/src/pkg/deps"

	"gserver/core/gxyredis"
	"gserver/core/gxyservice/gxyservicetest"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// SQL 语句(依赖经 deps 注入,不打招呼就出现的话 sqlmock 会直接报错)。
const (
	sqlPlotStolenCount   = `SELECT count\(\*\) FROM "steal_record" WHERE owner_id = \$1 AND plot_id = \$2`
	sqlStealRecordExists = `SELECT count\(\*\) FROM "steal_record" WHERE stealer_id = \$1 AND owner_id = \$2 AND plot_id = \$3`
	sqlCreateStealRecord = `INSERT INTO "steal_record" \("owner_id","plot_id","stealer_id","flower_id","steal_time"\) VALUES \(\$1,\$2,\$3,\$4,\$5\) RETURNING "id"`
)

func setupTestSteal(t *testing.T) (*RoleSteal, sqlmock.Sqlmock, *miniredis.Miniredis) {
	t.Helper()
	plotCfgInited = false
	initPlotTestConfig(t)

	db, mock := newGormMock(t)
	mr := miniredis.RunT(t)
	cli := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = cli.Close() })

	main := &RoleMain{RoleID: 1001, deps: deps.Deps{DB: db, Redis: cli}}
	bagMod := &RoleBag{
		RoleModule:   RoleModule{RoleID: main.RoleID, Role: main},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	stealMod := &RoleSteal{
		RoleModule:     RoleModule{RoleID: main.RoleID, Role: main},
		RoleStealState: RoleStealState{RolePersistState: RolePersistState{RoleID: main.RoleID}},
	}
	main.Bag = bagMod
	main.Steal = stealMod
	if err := stealMod.OnModInit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return stealMod, mock, mr
}

// serveFriendDouble 装上 friend 服务替身,让准入判定走 HTTP 而非直查表。
func serveFriendDouble(t *testing.T, friendIDs map[int64]bool) {
	t.Helper()
	reg := gxyservicetest.Install(t)
	reg.Serve(t, "friend", &fakeFriendHandler{isFriendIDs: friendIDs})
}

// expectFriendAndStealReads 声明读路径的四条 SQL:是好友、(该地)未被偷过、(我)没偷过。
func expectFriendAndStealReads(mock sqlmock.Sqlmock, friendID int64, stolen int64) {
	mock.ExpectQuery(sqlPlotStolenCount).WithArgs(friendID, int32(plotTestID)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(stolen))
	mock.ExpectQuery(sqlStealRecordExists).WithArgs(int64(1001), friendID, int32(plotTestID)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
}

func publishHarvestablePlotSnapshot(cli gxyredis.Client, roleID int64) {
	publishRolePlotSnapshot(context.Background(), cli, roleID, PlotMap{
		plotTestID: {
			PlotID:       plotTestID,
			FlowerID:     plotTestFlower,
			State:        int32(pb.PlotState_PLOT_GROWING),
			HarvestCount: 0,
			StateTime:    time.Now().Add(-time.Minute),
		},
	})
}

// TestReqPlotFriendInfoReadsSnapshot:好友地块列表与 CanSteal 只能来自好友发布的 Redis 快照,sqlmock 期望未被 DB 查询消耗。
// 为什么需要:快照是刚收获完的实时状态。若改成直查 DB,CanSteal 会慢一次 flush,
// 地块明明已收获仍显示可偷(点了报无货),或已偷过的地块仍显示可偷,导致重复偷取。
func TestReqPlotFriendInfoReadsSnapshot(t *testing.T) {
	steal, mock, _ := setupTestSteal(t)
	friendID := int64(2002)
	serveFriendDouble(t, map[int64]bool{friendID: true})
	publishHarvestablePlotSnapshot(steal.Redis(), friendID)
	expectFriendAndStealReads(mock, friendID, 0)

	rsp, err := steal.ReqPlotFriendInfo(context.Background(), &pb.ReqPlotFriendInfo{FriendId: friendID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Plots) != 1 {
		t.Fatalf("expected 1 plot, got %d", len(rsp.Plots))
	}
	if rsp.Plots[0].State != pb.PlotState_PLOT_HARVESTABLE {
		t.Fatalf("expected harvestable, got %v", rsp.Plots[0].State)
	}
	if !rsp.Plots[0].CanSteal {
		t.Fatal("expected can_steal from snapshot state")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}

// TestCallFriendIsFriend_GoesThroughService:准入判定必须经 friend 服务,不再直查
// friend_relation 表。
// 为什么需要:该表由 friend 应用持有。role 直查属于跨应用越界——friend 改表结构时
// role 编译照过、运行时才炸,且两份好友判定会各自漂移。此处用「服务不可用 → false」
// 钉住 fail-closed 语义:friend 挂掉时宁可拒绝一次操作,也不放行未经校验的准入。
func TestCallFriendIsFriend_GoesThroughService(t *testing.T) {
	reg := gxyservicetest.Install(t)
	reg.Serve(t, "friend", &fakeFriendHandler{isFriendIDs: map[int64]bool{2002: true}})
	ctx := context.Background()

	if !callFriendIsFriend(ctx, 1001, 2002) {
		t.Fatal("2002 should be a friend of 1001")
	}
	if callFriendIsFriend(ctx, 1001, 3003) {
		t.Fatal("3003 should not be a friend of 1001")
	}
}

// TestCallFriendIsFriend_FailsClosedOnServiceError:friend 服务不可用时必须返回 false。
// 为什么需要:三个调用点(steal ×2、私聊 ×1)都拿这个布尔当准入闸门;返回 true 会让
// 非好友通过准入,这是越权。停用服务即验证。
func TestCallFriendIsFriend_FailsClosedOnServiceError(t *testing.T) {
	// 只装注册表、不 Serve 任何 friend → 调用必然失败
	gxyservicetest.Install(t)
	ctx := context.Background()

	// 未 Serve 任何 friend → 调用必然失败
	if callFriendIsFriend(ctx, 1001, 2002) {
		t.Fatal("must fail closed when friend service is unavailable")
	}
}

// TestReqPlotStealUsesPlotLock:偷取全程在 plotLockKey(owner,plot) 锁内完成写 steal_record,返回后锁键必须消失。
// 为什么需要:锁把"校验可偷 + 写记录"变成临界区,是并发偷取不产生两条 steal_record 的唯一保障;
// 返回后仍有残留锁键会让这块地再也无法被任何人偷。
func TestReqPlotStealUsesPlotLock(t *testing.T) {
	steal, mock, mr := setupTestSteal(t)
	friendID := int64(2002)
	serveFriendDouble(t, map[int64]bool{friendID: true})
	publishHarvestablePlotSnapshot(steal.Redis(), friendID)

	expectFriendAndStealReads(mock, friendID, 0)
	mock.ExpectBegin()
	mock.ExpectQuery(sqlCreateStealRecord).
		WithArgs(friendID, int32(plotTestID), int64(1001), plotTestFlower, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	rsp, err := steal.ReqPlotSteal(context.Background(), &pb.ReqPlotSteal{FriendId: friendID, PlotId: plotTestID})
	if err != nil {
		t.Fatal(err)
	}
	if !rsp.Success {
		t.Fatal("expected success")
	}
	if mr.Exists(plotLockKey(friendID, plotTestID)) {
		t.Fatalf("expected lock released, key still present: %s", plotLockKey(friendID, plotTestID))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}
