package packet

// LTIV/LTPV 编解码器测试:纯函数级,构造结构体直接测,覆盖编解码往返与边界条件。

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"gserver/core/gxynet/message"
)

// newLtivForTest 用生产线格式配置构造(与 config/gate.toml 的
// size_length=2/type_length=1/id_length=2/max_size=65535 一致),
// 避免测试覆盖生产不存在的线格式。
func newLtivForTest(order binary.ByteOrder) *ltiv {
	return &ltiv{
		byteOrder: order,
		conf: &ltivConfig{
			SizeLength: 2, MaxSize: 65535,
			TypeLength: 1, IDLength: 2,
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
		// 2(size) + 1(type) + 2(id) + 5(payload)
		if len(data) != 10 {
			t.Fatalf("expected 10 bytes, got %d", len(data))
		}

		consumed, decoded, err := l.Decode(data)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if consumed != 10 {
			t.Fatalf("expected consume 10, got %d", consumed)
		}
		if decoded.Type != 7 || decoded.Path != "1001" || string(decoded.Payload) != "hello" {
			t.Fatalf("roundtrip mismatch: %+v", decoded)
		}
	}
}

func TestLtiv_Decode_HeadNotEnough(t *testing.T) {
	l := newLtivForTest(binary.BigEndian)
	// 少于 SizeLength(2)
	_, _, err := l.Decode([]byte{0x01})
	if !errors.Is(err, ErrPkgHeadNotEnough) {
		t.Fatalf("expected ErrPkgHeadNotEnough, got %v", err)
	}
}

func TestLtiv_Decode_BodyNotEnough(t *testing.T) {
	l := newLtivForTest(binary.BigEndian)
	data := make([]byte, 6) // 声明 size 但 body 不足
	binary.BigEndian.PutUint16(data[:2], 100)
	_, _, err := l.Decode(data)
	if !errors.Is(err, ErrPkgBodyNotEnough) {
		t.Fatalf("expected ErrPkgBodyNotEnough, got %v", err)
	}
}

func TestLtiv_Decode_PacketTooBig(t *testing.T) {
	// 生产配置(size_length=2/max_size=65535)下该检查不可达: 长度字段
	// 天然上限 65535 == max_size, dataSize 永远不超。此处用独立小上限
	// 覆盖报错逻辑本身。
	l := &ltiv{
		byteOrder: binary.BigEndian,
		conf: &ltivConfig{
			SizeLength: 2, MaxSize: 100,
			TypeLength: 1, IDLength: 2,
		},
	}
	data := make([]byte, 2)
	binary.BigEndian.PutUint16(data[:2], 101)
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
	if len(data) != 5 { // 2 + 1 + 2 + 0
		t.Fatalf("expected 5 bytes, got %d", len(data))
	}
	_, decoded, err := l.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Path != "1" || len(decoded.Payload) != 0 {
		t.Fatalf("unexpected: %+v", decoded)
	}
}

// TestLtiv_Encode_TooBig 生产配置(size_length=2/max_size=65535)下,
// payload 超过长度字段上限必须报错而非静默截断(旧行为 uint16 mod 产生损坏包)。
func TestLtiv_Encode_TooBig(t *testing.T) {
	l := newLtivForTest(binary.BigEndian)
	msg := &message.Message{Type: 1, Path: "1", Payload: make([]byte, 65536)}
	_, err := l.Encode(msg)
	if err == nil || !strings.Contains(err.Error(), "packet too big") {
		t.Fatalf("expected packet too big error, got %v", err)
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

// TestLtpv_Encode_TooBig payload 超过 max_size(1024) 必须报错而非截断。
func TestLtpv_Encode_TooBig(t *testing.T) {
	l := newLtpvForTest(binary.BigEndian)
	msg := &message.Message{Type: 1, Path: "p", Payload: make([]byte, 1025)}
	_, err := l.Encode(msg)
	if err == nil || !strings.Contains(err.Error(), "packet too big") {
		t.Fatalf("expected packet too big error, got %v", err)
	}
}
