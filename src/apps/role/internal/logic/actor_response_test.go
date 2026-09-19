package logic

import (
	"testing"

	"gserver/protocol/pb"
)

// 载荷不是期望类型时必须返回错误,而不是让调用方在断言上 panic——
// 一次断言失败会让整个 role actor 终止、玩家掉线。
func TestAsResponseRejectsUnexpectedPayload(t *testing.T) {
	if _, err := asResponse[*pb.RspGuildInfo](&pb.ActorError{Reason: "boom"}); err == nil {
		t.Fatal("载荷类型不符时必须返回错误")
	}
}

// 正常载荷必须原样返回,不得被误判。
func TestAsResponseAcceptsExpectedPayload(t *testing.T) {
	want := &pb.RspGuildInfo{Guild: &pb.PGuildBasic{Id: 7}}
	got, err := asResponse[*pb.RspGuildInfo](want)
	if err != nil {
		t.Fatalf("正常载荷不应报错: %v", err)
	}
	if got != want {
		t.Fatalf("返回的载荷不是原对象: %#v", got)
	}
}
