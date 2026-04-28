package client

import (
	"net"
	"testing"
	"time"

	pb "hy_client/pb"

	"google.golang.org/protobuf/proto"
)

func TestClientHandshakeLogin(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		codec := NewLTIVCodec()
		buf := make([]byte, 8192)

		// Read handshake request
		n, _ := conn.Read(buf)
		_, msg, _ := codec.Decode(buf[:n])
		if msg.Path != "10001" {
			return
		}

		// Send handshake response
		rsp := &pb.RspHandShake{AccountUid: "test", RoleId: 1001}
		payload, _ := proto.Marshal(rsp)
		data, _ := codec.Encode(&Message{Type: 1, Path: "10002", Payload: payload})
		conn.Write(data)

		// Read login request
		n, _ = conn.Read(buf)
		_, msg, _ = codec.Decode(buf[:n])
		if msg.Path != "10003" {
			return
		}

		// Send login response
		loginRsp := &pb.RspAccountLogin{FirstLogin: false, RoleId: 1001}
		payload, _ = proto.Marshal(loginRsp)
		data, _ = codec.Encode(&Message{Type: 1, Path: "10004", Payload: payload})
		conn.Write(data)
	}()

	cfg := Config{Addr: listener.Addr().String(), AccountUID: "test"}
	c := NewClient(cfg)

	err = c.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	rsp, err := c.Handshake()
	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	if rsp.RoleId != 1001 {
		t.Errorf("role_id mismatch: got %d, want 1001", rsp.RoleId)
	}

	loginRsp, err := c.Login()
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if loginRsp.RoleId != 1001 {
		t.Errorf("login role_id mismatch: got %d, want 1001", loginRsp.RoleId)
	}
}

func TestClientRequestResponse(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		codec := NewLTIVCodec()
		buf := make([]byte, 8192)

		n, _ := conn.Read(buf)
		_, msg, _ := codec.Decode(buf[:n])

		reqID := pathToUint16(msg.Path)
		rspID := reqID + 1
		rspPayload := []byte{0x08, 0x01}
		data, _ := codec.Encode(&Message{Type: 1, Path: uint16ToID(rspID), Payload: rspPayload})
		conn.Write(data)
	}()

	cfg := Config{Addr: listener.Addr().String()}
	c := NewClient(cfg)
	err = c.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err = c.Request(&pb.ReqBagInfo{})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
}

func TestClientNotification(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	notifyCh := make(chan proto.Message, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		codec := NewLTIVCodec()
		notify := &pb.NotifyBagUpdate{}
		payload, _ := proto.Marshal(notify)
		data, _ := codec.Encode(&Message{Type: 1, Path: "21003", Payload: payload})
		conn.Write(data)
	}()

	cfg := Config{Addr: listener.Addr().String()}
	c := NewClient(cfg)
	c.OnMessage(func(msg proto.Message) {
		notifyCh <- msg
	})

	err = c.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	select {
	case msg := <-notifyCh:
		if _, ok := msg.(*pb.NotifyBagUpdate); !ok {
			t.Errorf("expected NotifyBagUpdate, got %T", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for notification")
	}
}
