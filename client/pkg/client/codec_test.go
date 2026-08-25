package client

import (
	"testing"
)

func TestEncodeDecodeHandshake(t *testing.T) {
	codec := NewLTIVCodec()

	payload := []byte{0x0a, 0x09, 0x74, 0x65, 0x73, 0x74, 0x5f, 0x75, 0x69, 0x64}
	msg := &Message{
		Type:    0,
		Path:    "10001",
		Payload: payload,
	}

	data, err := codec.Encode(msg)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	if len(data) < 5 {
		t.Fatalf("encoded data too short: %d bytes", len(data))
	}

	expectedSize := 1 + 2 + len(payload)
	actualSize := int(data[0]) | int(data[1])<<8
	if actualSize != expectedSize {
		t.Errorf("size mismatch: got %d, want %d", actualSize, expectedSize)
	}

	if data[2] != 0 {
		t.Errorf("type mismatch: got %d, want 0", data[2])
	}

	consumed, decoded, err := codec.Decode(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if consumed != len(data) {
		t.Errorf("consumed mismatch: got %d, want %d", consumed, len(data))
	}
	if decoded.Type != 0 {
		t.Errorf("decoded type: got %d, want 0", decoded.Type)
	}
	if decoded.Path != "10001" {
		t.Errorf("decoded path: got %s, want 10001", decoded.Path)
	}
}

func TestDecodePartialData(t *testing.T) {
	codec := NewLTIVCodec()

	_, _, err := codec.Decode([]byte{0x01})
	if err != ErrHeadNotEnough {
		t.Errorf("expected ErrHeadNotEnough, got: %v", err)
	}

	_, _, err = codec.Decode([]byte{0x0a, 0x00, 0x01})
	if err != ErrBodyNotEnough {
		t.Errorf("expected ErrBodyNotEnough, got: %v", err)
	}
}

func TestEncodeDecodeDataPacket(t *testing.T) {
	codec := NewLTIVCodec()

	payload := []byte{0x01, 0x02, 0x03}
	msg := &Message{
		Type:    1,
		Path:    "21001",
		Payload: payload,
	}

	data, err := codec.Encode(msg)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	consumed, decoded, err := codec.Decode(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if consumed != len(data) {
		t.Errorf("consumed: got %d, want %d", consumed, len(data))
	}
	if decoded.Type != 1 {
		t.Errorf("type: got %d, want 1", decoded.Type)
	}
	if decoded.Path != "21001" {
		t.Errorf("path: got %s, want 21001", decoded.Path)
	}
}
