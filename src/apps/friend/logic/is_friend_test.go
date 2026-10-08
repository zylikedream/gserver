package logic

// friend /is_friend 接口测试:role 侧的 steal 与私聊准入都经此判定「两人是否好友」,
// 这条表只归 friend 持有(role 此前直查,属跨应用越界)。

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const sqlIsFriendCount = `SELECT count\(\*\) FROM "friend_relation" WHERE player_id = \$1 AND friend_id = \$2`

// TestIsFriend_Hit:关系存在时返回 {"is_friend": true}。
// 为什么需要:role 拿这个布尔当准入闸门——返回 false 会让好友之间互相看不见地块、
// 也发不出私聊,且客户端只看到一个笼统的「不是好友」提示。
func TestIsFriend_Hit(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	mock.ExpectQuery(sqlIsFriendCount).WithArgs(int64(1001), int64(2002)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	h := &FriendHandler{db: gormDB}
	data, err := h.IsFriend(context.Background(), &IsFriendReq{PlayerID: 1001, TargetID: 2002})
	if err != nil {
		t.Fatalf("IsFriend: %v", err)
	}
	m, ok := data.(map[string]bool)
	if !ok || !m["is_friend"] {
		t.Fatalf("expected {is_friend:true}, got %#v", data)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}

// TestIsFriend_Miss:关系不存在时返回 false,且不得报错。
// 为什么需要:「不是好友」是正常业务结果而非异常。回错误会让 role 侧无法区分
// 「不是好友」与「服务故障」,进而把正常的拒绝记成故障。
func TestIsFriend_Miss(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	mock.ExpectQuery(sqlIsFriendCount).WithArgs(int64(1001), int64(3003)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	h := &FriendHandler{db: gormDB}
	data, err := h.IsFriend(context.Background(), &IsFriendReq{PlayerID: 1001, TargetID: 3003})
	if err != nil {
		t.Fatalf("IsFriend: %v", err)
	}
	m, ok := data.(map[string]bool)
	if !ok || m["is_friend"] {
		t.Fatalf("expected {is_friend:false}, got %#v", data)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}

// TestIsFriend_QueryIsDirectional:本接口只查 (player_id, friend_id) 单行,所以
// 两个方向必须分别查——对称性由 addRelation/removeRelation 维护两行来保证。
// 为什么需要:若哪天把它改成"查任一方向",上面的方向性断言就不再有意义,且
// role 侧会拿到与 friend 侧不一致的答案。用两条不同的期望钉住单行查询。
func TestIsFriend_QueryIsDirectional(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	// 反向查询的参数顺序相反,且各自独立命中
	mock.ExpectQuery(sqlIsFriendCount).WithArgs(int64(2002), int64(1001)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(sqlIsFriendCount).WithArgs(int64(3003), int64(1001)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	h := &FriendHandler{db: gormDB}
	ctx := context.Background()
	if data, err := h.IsFriend(ctx, &IsFriendReq{PlayerID: 2002, TargetID: 1001}); err != nil {
		t.Fatal(err)
	} else if m := data.(map[string]bool); !m["is_friend"] {
		t.Fatalf("expected reverse direction to be a friend too, got %#v", data)
	}
	if data, err := h.IsFriend(ctx, &IsFriendReq{PlayerID: 3003, TargetID: 1001}); err != nil {
		t.Fatal(err)
	} else if m := data.(map[string]bool); m["is_friend"] {
		t.Fatalf("expected stranger, got %#v", data)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}

// TestIsFriend_QueryErrorMapsToErrCode:DB 故障必须回错码而不是 false。
// 为什么需要:role 侧对这两种结果的处置不同(错误 → 记日志并 fail-closed;
// false → 静默拒绝)。若这里把 DB 错误也答成 {"is_friend":false},role 无法察觉
// friend 侧正在故障,排障时会误以为是用户关系问题。
func TestIsFriend_QueryErrorMapsToErrCode(t *testing.T) {
	gormDB, mock := newFriendDBMock(t)
	mock.ExpectQuery(sqlIsFriendCount).WithArgs(int64(1001), int64(2002)).
		WillReturnError(sqlmock.ErrCancelled)

	h := &FriendHandler{db: gormDB}
	data, err := h.IsFriend(context.Background(), &IsFriendReq{PlayerID: 1001, TargetID: 2002})
	if err == nil {
		t.Fatal("expected error on DB failure")
	}
	if data != nil {
		t.Fatalf("expected nil data on error, got %#v", data)
	}
}
