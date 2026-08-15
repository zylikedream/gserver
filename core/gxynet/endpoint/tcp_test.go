package endpoint

import (
	"net"
	"testing"

	"gserver/core/gxynet/message"
)

type testProcessor struct{}

func (p testProcessor) Decode(data []byte) (uint64, *message.Message, error) {
	if len(data) < 2 {
		return 0, nil, nil
	}
	return 2, &message.Message{}, nil
}

func (p testProcessor) Encode(msg *message.Message) ([]byte, error) {
	return nil, nil
}

func TestTcpEndpointDecodeMsgUsesOnlyProvidedData(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	t.Cleanup(func() { _ = server.Close() })

	ep := NewTcpEndPoint(server, testProcessor{})

	msg, n, err := ep.DecodeMsg([]byte{1, 2, 3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || n != 2 {
		t.Fatalf("first DecodeMsg got msg=%v n=%d, want msg and n=2", msg, n)
	}

	msg, n, err = ep.DecodeMsg([]byte{3, 4})
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || n != 2 {
		t.Fatalf("second DecodeMsg got msg=%v n=%d, want msg and n=2", msg, n)
	}

	msg, n, err = ep.DecodeMsg(nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg != nil || n != 0 {
		t.Fatalf("third DecodeMsg got msg=%v n=%d, want no msg and n=0", msg, n)
	}
}
