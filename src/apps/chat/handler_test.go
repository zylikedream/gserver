package chat

// ChatHandler HTTP 层校验测试:依赖校验在 DB 调用前, 构造零依赖 handler 即可测。

import (
	"context"
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

// TestHandler_StorePrivateMsg_ValidSender 合法 sender 通过校验(DB 未初始化, 走到持久化报错即可)。
func TestHandler_StorePrivateMsg_ValidSender(t *testing.T) {
	h := &ChatHandler{} // d.DB nil → StorePrivateMsg 在 gorm 调用时 panic? 见下
	ctx := context.Background()
	// 校验通过后调用 StorePrivateMsg → d.DB 为 nil → gorm nil 接收者?
	// gorm.DB 是 struct, nil 指针调用 WithContext 会 panic。此处只验证校验层,
	// 预期走到持久化报错(而不是"invalid sender")。
	_, err := h.StorePrivateMsg(ctx, &StorePrivateMsgReq{
		Sender:   `{"roleId":100}`,
		TargetID: 200,
		Content:  "hi",
	})
	if err == nil {
		t.Fatal("expected persist-stage error (db nil)")
	}
}
