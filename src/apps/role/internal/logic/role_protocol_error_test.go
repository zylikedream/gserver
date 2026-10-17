package logic

import (
	"context"
	"testing"

	"gserver/core/gxyactor"
	"gserver/protocol/pb"
)

// 未注册的客户端消息必须走协议错误路径:回 ACK_CODE_ERROR、result=error。
//
// 为什么需要:这条路径曾经被无声破坏过——同步入口在"没有对应处理器"时返回
// (nil, nil),于是 res==nil 落到 result="ok",协议错误既不记日志也不回错误应答,
// 客户端只能自己超时,而指标 client_requests_total.result 从 error 变成 ok。
// 当时全仓没有任何用例覆盖这三行接线,回归无人发现。
func TestUnregisteredClientMsgReturnsProtocolError(t *testing.T) {
	main := &RoleMain{RoleID: 1001, state: RoleStateLogined}
	main.EntityActor = gxyactor.NewEntityActor()

	// pb.Ack 是服务端→客户端的类型,不会注册在客户端消息分派表里。
	// 它代表"协议里存在、但这个 actor 没有对应处理器"的消息。
	rsp, result, err := main.dispatchClientMsg(context.Background(), "req-1", &pb.Ack{}, "unknown.id", "UnknownMsg")
	if err != nil {
		t.Fatalf("协议错误路径不应返回 error: %v", err)
	}
	if result != "error" {
		t.Fatalf("result = %q, want \"error\"(未注册消息不得计成 ok)", result)
	}
	if rsp == nil {
		t.Fatal("必须回一个 error Ack,否则客户端只能等到超时")
	}

	svrMsg, ok := rsp.(*pb.ServerMsg)
	if !ok {
		t.Fatalf("应答类型 = %T, want *pb.ServerMsg", rsp)
	}
	ack := &pb.Ack{}
	if err := svrMsg.Msg.UnmarshalTo(ack); err != nil {
		t.Fatalf("应答不是 Ack: %v", err)
	}
	if ack.GetCode() != pb.AckCode_ACK_CODE_ERROR {
		t.Fatalf("Ack code = %v, want ACK_CODE_ERROR", ack.GetCode())
	}
	if ack.GetReason() == "" {
		t.Fatal("Ack reason 必须说明是哪条路由缺失")
	}
}
