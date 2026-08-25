package client

import (
	"net"
	"testing"
	"time"
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

func TestConnDetectServerDisconnect(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	disconnected := make(chan struct{}, 1)
	conn := NewConn(func(msg *Message) {
		if msg == nil {
			disconnected <- struct{}{}
		}
	})

	err = conn.Connect(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	// Accept the server side and immediately close
	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	serverConn.Close()
	listener.Close()

	select {
	case <-disconnected:
		// Success! Disconnect was detected.
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: disconnect not detected within 3 seconds")
	}
}

func TestClientDetectServerDisconnect(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	disconnected := make(chan error, 1)
	client := NewClient(Config{})
	client.onDisconnect = func(reason error) {
		disconnected <- reason
	}

	client.conn = NewConn(client.handleMessage)
	err = client.conn.Connect(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	// Accept server side and close it
	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	serverConn.Close()
	listener.Close()

	select {
	case reason := <-disconnected:
		if reason == nil {
			t.Error("expected non-nil error reason")
		}
		// Success!
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: client disconnect not detected within 3 seconds")
	}

	if !client.IsConnected() {
		t.Log("client correctly reports not connected")
	} else {
		t.Error("client should report not connected after disconnect")
	}
}
