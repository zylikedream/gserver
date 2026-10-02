package chat

// ChatHandler HTTP 层校验测试:依赖校验在 DB 调用前, 构造零依赖 handler 即可测。

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"gserver/src/pkg/deps"
	"testing"
)

// TestHandler_StorePrivateMsg_InvalidSender sender 缺 role_id 或为 null 必须拒绝。
func TestHandler_StorePrivateMsg_InvalidSender(t *testing.T) {
	h := &ChatHandler{} // 零依赖: 校验发生在 DB 访问之前
	ctx := context.Background()

	cases := []struct {
		name   string
		sender string
	}{
		{"null sender", "null"},
		{"missing role_id", `{"roleId":0}`},
		{"empty object", `{}`},
	}
	for _, c := range cases {
		if _, err := h.StorePrivateMsg(ctx, &StorePrivateMsgReq{
			Sender:   c.sender,
			TargetID: 100,
			Content:  "hi",
		}); err == nil {
			t.Fatalf("[%s] expected error for invalid sender", c.name)
		}
	}
}

// TestHandler_StorePrivateMsg_ValidSender sender 合法时必须真正走到持久化:
// 用 sqlmock 断言 INSERT 被发出,而不是"返回了任意错误"。
// 这是与 InvalidSender 的分界——校验放行之后,DB 才是下一个必经步骤。
func TestHandler_StorePrivateMsg_ValidSender(t *testing.T) {
	gormDB, mock := newChatDBMock(t)
	h := &ChatHandler{d: deps.Deps{DB: gormDB}}

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "chat_private_message"`).
		WithArgs(int64(100), int64(200), int64(100), "hi", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	rsp, err := h.StorePrivateMsg(context.Background(), &StorePrivateMsgReq{
		Sender:   `{"role_id":100}`,
		TargetID: 200,
		Content:  "hi",
	})
	if err != nil {
		t.Fatalf("valid sender must reach persist, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("store not reached: %v", err)
	}
	m, ok := rsp.(map[string]int64)
	if !ok || m["timestamp"] <= 0 {
		t.Fatalf("unexpected response: %#v", rsp)
	}
}
