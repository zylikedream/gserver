package client

import (
	"testing"

	"hy_client/pb"
)

func TestRegistryRegistersAllMessages(t *testing.T) {
	RegisterMessages()

	testCases := []struct {
		id   string
		name string
	}{
		{"10001", "ReqHandShake"},
		{"10002", "RspHandShake"},
		{"10003", "ReqAccountLogin"},
		{"10004", "RspAccountLogin"},
		{"20001", "ReqBasicSetName"},
		{"21001", "ReqBagInfo"},
		{"22001", "ReqGMCommand"},
		{"23001", "ReqFlowerInfo"},
		{"25001", "ReqMainTaskInfo"},
		{"25003", "ReqClaimMainTask"},
		{"26001", "ReqOrderInfo"},
		{"26003", "ReqSubmitOrder"},
	}

	for _, tc := range testCases {
		msgType := MessageTypeByID(tc.id)
		if msgType == nil {
			t.Errorf("no message registered for id %s", tc.id)
			continue
		}
		if msgType.Name() != tc.name {
			t.Errorf("id %s: got name %s, want %s", tc.id, msgType.Name(), tc.name)
		}
	}
}

func TestRegistryBidirectional(t *testing.T) {
	RegisterMessages()

	msg := &pb.ReqHandShake{}
	id := IDByMessage(msg)
	if id != "10001" {
		t.Errorf("ReqHandShake ID: got %s, want 10001", id)
	}
}

func TestRegistryNewInstance(t *testing.T) {
	RegisterMessages()

	msg := NewMessageByID("10001")
	if msg == nil {
		t.Fatal("expected non-nil message for id 10001")
	}
	handshake, ok := msg.(*pb.ReqHandShake)
	if !ok {
		t.Fatal("expected *pb.ReqHandShake")
	}
	handshake.AccountUid = "test"
	if handshake.AccountUid != "test" {
		t.Error("failed to set field on new instance")
	}
}
