package logic

// 发给客户端的消息在运行时边界上观测:actor 测试基座记录 egress,
// 测试用 ShouldSend 断言"发了什么"。生产侧没有为测试留任何接缝
// (见 docs/architecture/invariants.md「测试替身规则」)。

import (
	"testing"

	"gserver/core/gxyactor"
	"gserver/core/gxyactor/gxyactortest"
	"gserver/protocol/pb"
	"gserver/src/lib"

	"ergo.services/ergo/testing/check"
	"ergo.services/ergo/testing/unit"
	"github.com/DATA-DOG/go-sqlmock"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// clientMsgMatcher 匹配发给客户端的载荷:解 anypb 后交给 pred 判定。
func clientMsgMatcher(pred func(proto.Message) bool) func(check.Send) bool {
	return func(s check.Send) bool {
		svr, ok := s.Message.(*pb.ServerMsg)
		if !ok || svr.Msg == nil {
			return false
		}
		got, err := anypb.UnmarshalNew(svr.Msg, proto.UnmarshalOptions{})
		if err != nil {
			return false
		}
		return pred(got)
	}
}

// spawnTestRole 起一个真 role actor(Init 会查 account 表),并把会话指向一个非零 PID,
// 使 SendClient 真正走发送路径。返回的 Subject 用于断言 egress。
func spawnTestRole(t *testing.T, roleID int64) (*RoleMain, *unit.Subject, sqlmock.Sqlmock) {
	t.Helper()
	initAllTestConfig(t)
	db, mock := newGormMock(t)
	mock.ExpectQuery(selectAccountByRoleID).WithArgs(roleID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "role_id"}).AddRow("acc_test", roleID))

	gxyactortest.StubOwnership(t)
	role, subj := gxyactortest.Spawn(t, lib.ROLE_ACTOR_TYPE, roleCtorWithDB(db), roleID)
	role.session = gxyactor.NewPID("gate-test", "session-test")
	return role, subj, mock
}
