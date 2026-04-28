package client

import (
	"net"
	"testing"
	"time"

	pb "hy_client/pb"

	"google.golang.org/protobuf/proto"
)

func TestConnSendAndReceive(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverCh := make(chan *Message, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		codec := NewLTIVCodec()
		consumed, msg, err := codec.Decode(buf[:n])
		if err != nil {
			return
		}
		if consumed != n {
			t.Errorf("server: consumed %d != n %d", consumed, n)
		}
		serverCh <- msg

		// Echo back a response
		rsp := &Message{
			Type:    1,
			Path:    "10002",
			Payload: []byte{0x0a, 0x04, 0x74, 0x65, 0x73, 0x74},
		}
		data, _ := codec.Encode(rsp)
		conn.Write(data)
	}()

	received := make(chan *Message, 1)
	conn := NewConn(func(msg *Message) {
		received <- msg
	})

	err = conn.Connect(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	err = conn.SendRaw(&Message{
		Type:    0,
		Path:    "10001",
		Payload: []byte{0x0a, 0x04, 0x74, 0x65, 0x73, 0x74},
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case srvMsg := <-serverCh:
		if srvMsg.Path != "10001" {
			t.Errorf("server received path %s, want 10001", srvMsg.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for server to receive")
	}

	select {
	case cliMsg := <-received:
		if cliMsg.Path != "10002" {
			t.Errorf("client received path %s, want 10002", cliMsg.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for client to receive")
	}
}

func TestConnSendProtobuf(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverCh := make(chan *Message, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		codec := NewLTIVCodec()
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		_, msg, _ := codec.Decode(buf[:n])
		serverCh <- msg
	}()

	conn := NewConn(func(msg *Message) {})
	err = conn.Connect(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	err = conn.Send(&pb.ReqHandShake{AccountUid: "test_account"})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case msg := <-serverCh:
		// Verify the server got a properly encoded handshake
		if msg.Path != "10001" {
			t.Errorf("path: got %s, want 10001", msg.Path)
		}
		req := &pb.ReqHandShake{}
		if err := proto.Unmarshal(msg.Payload, req); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if req.AccountUid != "test_account" {
			t.Errorf("account_uid: got %s, want test_account", req.AccountUid)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
