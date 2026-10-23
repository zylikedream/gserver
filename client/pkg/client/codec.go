package client

import (
	"encoding/binary"
	"errors"
)

// 线上契约:与 config/gate.toml 的 [packet.ltiv] 保持一致
// (size_length=2 / type_length=1 / id_length=2 / byte_order=little / max_size=65535)。
// client 模块被门禁禁止 import 服务端实现,两侧靠 packet_test.go 与 codec_test.go
// 里相同的黄金帧字面量互相锁定。
const (
	sizeLen = 2
	typeLen = 1
	idLen   = 2
	// 2B 长度字段上限,与 [packet.ltiv] max_size 一致;超出必须编码前拦截。
	maxSize = 65535
)

var (
	ErrHeadNotEnough = errors.New("packet header not enough")
	ErrBodyNotEnough = errors.New("packet body not enough")
	ErrPacketTooBig  = errors.New("packet too big")
)

type Message struct {
	Type    uint16
	Path    string // message ID as string, e.g. "10001"
	Payload []byte
}

type LTIVCodec struct{}

func NewLTIVCodec() *LTIVCodec {
	return &LTIVCodec{}
}

func (c *LTIVCodec) Encode(msg *Message) ([]byte, error) {
	// body = Type(1B) + ID(2B) + Payload
	body := make([]byte, 0, typeLen+idLen+len(msg.Payload))
	body = append(body, byte(msg.Type))

	pathNum := pathToUint16(msg.Path)
	idBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(idBuf, pathNum)
	body = append(body, idBuf...)
	body = append(body, msg.Payload...)
	if len(body) > maxSize {
		return nil, ErrPacketTooBig
	}

	// packet = Size(2B) + body
	sizeBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(sizeBuf, uint16(len(body)))

	result := make([]byte, 0, sizeLen+len(body))
	result = append(result, sizeBuf...)
	result = append(result, body...)
	return result, nil
}

func (c *LTIVCodec) Decode(data []byte) (int, *Message, error) {
	if len(data) < sizeLen {
		return 0, nil, ErrHeadNotEnough
	}

	bodySize := int(binary.LittleEndian.Uint16(data[:sizeLen]))
	if bodySize > maxSize {
		return 0, nil, ErrPacketTooBig
	}

	if len(data)-sizeLen < bodySize {
		return 0, nil, ErrBodyNotEnough
	}

	body := data[sizeLen : sizeLen+bodySize]
	if len(body) < typeLen+idLen {
		return 0, nil, errors.New("body too short")
	}

	msgType := uint16(body[0])
	msgID := binary.LittleEndian.Uint16(body[typeLen : typeLen+idLen])
	payload := body[typeLen+idLen:]

	return sizeLen + bodySize, &Message{
		Type:    msgType,
		Path:    uint16ToID(msgID),
		Payload: payload,
	}, nil
}

func pathToUint16(s string) uint16 {
	var result uint16
	for _, ch := range s {
		result = result*10 + uint16(ch-'0')
	}
	return result
}

func uint16ToID(id uint16) string {
	if id == 0 {
		return "0"
	}
	digits := make([]byte, 0, 5)
	for id > 0 {
		digits = append([]byte{byte('0' + id%10)}, digits...)
		id /= 10
	}
	return string(digits)
}
