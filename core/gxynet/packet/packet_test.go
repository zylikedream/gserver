package packet

// LTIV/LTPV 编解码器测试:纯函数级,构造结构体直接测,覆盖编解码往返与边界条件。

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"gserver/core/gxynet/message"
)

func newLtivForTest(order binary.ByteOrder) *ltiv {
	return &ltiv{
		byteOrder: order,
		conf: &ltivConfig{
			SizeLength: 4, MaxSize: 1024,
			TypeLength: 2, IDLength: 4,
		},
	}
}

func newLtpvForTest(order binary.ByteOrder) *ltpv {
	return &ltpv{
		byteOrder: order,
		conf: &ltpvConfig{
			SizeLength: 4, MaxSize: 1024,
			TypeLength: 2, PathLength: 2,
		},
	}
}

// ========== LTIV: length + type(uint16) + id(uint32) + payload ==========

func TestLtiv_EncodeDecode_RoundTrip(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.BigEndian, binary.LittleEndian} {
		l := newLtivForTest(order)
		msg := &message.Message{Type: 7, Path: "1001", Payload: []byte("hello")}

		data, err := l.Encode(msg)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		// 4(size) + 2(type) + 4(id) + 5(payload)
		if len(data) != 15 {
			t.Fatalf("expected 15 bytes, got %d", len(data))
		}

		consumed, decoded, err := l.Decode(data)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if consumed != 15 {
			t.Fatalf("expected consume 15, got %d", consumed)
		}
		if decoded.Type != 7 || decoded.Path != "1001" || string(decoded.Payload) != "hello" {
			t.Fatalf("roundtrip mismatch: %+v", decoded)
		}
	}
}

func TestLtiv_Decode_HeadNotEnough(t *testing.T) {
	l := newLtivForTest(binary.BigEndian)
	// 少于 SizeLength(4)
	_, _, err := l.Decode([]byte{0x00, 0x01})
	if !errors.Is(err, ErrPkgHeadNotEnough) {
		t.Fatalf("expected ErrPkgHeadNotEnough, got %v", err)
	}
}

func TestLtiv_Decode_BodyNotEnough(t *testing.T) {
	l := newLtivForTest(binary.BigEndian)
	data := make([]byte, 8) // 声明 size 但 body 不足
	binary.BigEndian.PutUint32(data[:4], 100)
	_, _, err := l.Decode(data)
	if !errors.Is(err, ErrPkgBodyNotEnough) {
		t.Fatalf("expected ErrPkgBodyNotEnough, got %v", err)
	}
}

func TestLtiv_Decode_PacketTooBig(t *testing.T) {
	l := newLtivForTest(binary.BigEndian)
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data[:4], uint32(l.conf.MaxSize)+1)
	_, _, err := l.Decode(data)
	if err == nil || !strings.Contains(err.Error(), "packet too big") {
		t.Fatalf("expected packet too big error, got %v", err)
	}
}

func TestLtiv_Encode_EmptyPayload(t *testing.T) {
	l := newLtivForTest(binary.BigEndian)
	msg := &message.Message{Type: 1, Path: "1"}
	data, err := l.Encode(msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(data) != 10 { // 4 + 2 + 4 + 0
		t.Fatalf("expected 10 bytes, got %d", len(data))
	}
	_, decoded, err := l.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Path != "1" || len(decoded.Payload) != 0 {
		t.Fatalf("unexpected: %+v", decoded)
	}
}

// ========== LTPV: length + type(uint16) + pathLen(uint16) + path + payload ==========

func TestLtpv_EncodeDecode_RoundTrip(t *testing.T) {
	for _, order := range []binary.ByteOrder{binary.BigEndian, binary.LittleEndian} {
		l := newLtpvForTest(order)
		msg := &message.Message{Type: 3, Path: "role.hello", Payload: []byte("world")}

		data, err := l.Encode(msg)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		// 4(size) + 2(type) + 2(pathLen=10) + 10(path) + 5(payload)
		expectedLen := 4 + 2 + 2 + 10 + 5
		if len(data) != expectedLen {
			t.Fatalf("expected %d bytes, got %d", expectedLen, len(data))
		}

		consumed, decoded, err := l.Decode(data)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if consumed != uint64(expectedLen) {
			t.Fatalf("expected consume %d, got %d", expectedLen, consumed)
		}
		if decoded.Type != 3 || decoded.Path != "role.hello" || string(decoded.Payload) != "world" {
			t.Fatalf("roundtrip mismatch: %+v", decoded)
		}
	}
}

func TestLtpv_Decode_BodyNotEnough(t *testing.T) {
	l := newLtpvForTest(binary.BigEndian)
	data := make([]byte, 8)
	binary.BigEndian.PutUint32(data[:4], 50)
	_, _, err := l.Decode(data)
	if !errors.Is(err, ErrPkgBodyNotEnough) {
		t.Fatalf("expected ErrPkgBodyNotEnough, got %v", err)
	}
}

func TestLtpv_Decode_HeadNotEnough(t *testing.T) {
	l := newLtpvForTest(binary.BigEndian)
	_, _, err := l.Decode(nil)
	if !errors.Is(err, ErrPkgHeadNotEnough) {
		t.Fatalf("expected ErrPkgHeadNotEnough, got %v", err)
	}
}

func TestLtpv_EncodeDecode_EmptyPath(t *testing.T) {
	l := newLtpvForTest(binary.BigEndian)
	msg := &message.Message{Type: 1, Path: "", Payload: []byte("x")}
	data, err := l.Encode(msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	_, decoded, err := l.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Path != "" || string(decoded.Payload) != "x" {
		t.Fatalf("unexpected: %+v", decoded)
	}
}
