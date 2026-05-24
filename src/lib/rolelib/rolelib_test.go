package rolelib

import (
	"encoding/json"
	"testing"
	"time"

	"gserver/protocol/pb"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestRoleNotifyTopic(t *testing.T) {
	got := roleNotifyTopic("game@123")
	want := "gserver:notify:role:game@123"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestRoleNotifyMsgJSONRoundTrip(t *testing.T) {
	notify := &pb.NotifyChatPrivate{
		Message: &pb.PChatMsg{
			Content:   "hello",
			Timestamp: time.Now().Unix(),
		},
	}
	anyMsg := &anypb.Any{}
	if err := anypb.MarshalFrom(anyMsg, notify, proto.MarshalOptions{}); err != nil {
		t.Fatalf("marshal any: %v", err)
	}
	payload, err := json.Marshal(&roleNotifyMsg{
		TargetRoleID: 123,
		Msg:          anyMsg,
		CreatedAt:    time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	decoded := &roleNotifyMsg{}
	if err := json.Unmarshal(payload, decoded); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	if decoded.TargetRoleID != 123 {
		t.Fatalf("expected target 123, got %d", decoded.TargetRoleID)
	}
	pbmsg, err := decoded.Msg.UnmarshalNew()
	if err != nil {
		t.Fatalf("unmarshal any: %v", err)
	}
	got, ok := pbmsg.(*pb.NotifyChatPrivate)
	if !ok {
		t.Fatalf("expected NotifyChatPrivate, got %T", pbmsg)
	}
	if got.Message.GetContent() != "hello" {
		t.Fatalf("expected content hello, got %s", got.Message.GetContent())
	}
}
