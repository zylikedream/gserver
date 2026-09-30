package logic

import (
	"context"
	"testing"
	"time"

	"gserver/protocol/pb"
	"gserver/src/pkg/deps"

	"gserver/core/gxyredis"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// SQL 语句(依赖经 deps 注入,不打招呼就出现的话 sqlmock 会直接报错)。
const (
	sqlFriendRelationCount = `SELECT count\(\*\) FROM "friend_relation" WHERE player_id = \$1 AND friend_id = \$2`
	sqlPlotStolenCount     = `SELECT count\(\*\) FROM "steal_record" WHERE owner_id = \$1 AND plot_id = \$2`
	sqlStealRecordExists   = `SELECT count\(\*\) FROM "steal_record" WHERE stealer_id = \$1 AND owner_id = \$2 AND plot_id = \$3`
	sqlCreateStealRecord   = `INSERT INTO "steal_record" \("owner_id","plot_id","stealer_id","flower_id","steal_time"\) VALUES \(\$1,\$2,\$3,\$4,\$5\) RETURNING "id"`
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

// expectFriendAndStealReads 声明读路径的四条 SQL:是好友、(该地)未被偷过、(我)没偷过。
func expectFriendAndStealReads(mock sqlmock.Sqlmock, friendID int64, stolen int64) {
	mock.ExpectQuery(sqlFriendRelationCount).WithArgs(int64(1001), friendID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
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

func TestReqPlotFriendInfoReadsSnapshot(t *testing.T) {
	steal, mock, _ := setupTestSteal(t)
	friendID := int64(2002)
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

func TestReqPlotStealUsesPlotLock(t *testing.T) {
	steal, mock, mr := setupTestSteal(t)
	friendID := int64(2002)
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
