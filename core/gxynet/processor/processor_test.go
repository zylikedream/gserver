package processor

// processor 编解码链路测试:构造注入 ltiv pktCodec + protobuf msgCodec,
// 验证 Encode→Decode 全链路(消息注册来自 codec 包 init 的 pb 全局注册)。

import (
	"encoding/binary"
	"testing"

	"github.com/cockroachdb/errors"

	"gserver/core/gxynet/codec"
	"gserver/core/gxynet/message"
	"gserver/core/gxynet/packet"
	pb "gserver/protocol/pb"
)

func newTestProcessor() *processor {
	return &processor{
		pktCodec: &testPacketCodec{},
		msgCodec: &codec.ProtoBuf{},
	}
}

// testPacketCodec:固定 SizeLength=4/BigEndian 的 LTIV 等价实现,避免依赖 gcfg 配置。
type testPacketCodec struct{}

func (c *testPacketCodec) Type() string { return "test.ltiv" }

func (c *testPacketCodec) Decode(data []byte) (uint64, *message.Message, error) {
	ltiv := &ltivHelper{}
	return ltiv.Decode(data)
}

func (c *testPacketCodec) Encode(msg *message.Message) ([]byte, error) {
	ltiv := &ltivHelper{}
	return ltiv.Encode(msg)
}

// ltivHelper 复用 packet 包内未导出的 ltiv 不可行(跨包),这里用等价构造。
// 注意:错误语义必须与生产 ltiv.Decode 一致——head 不足返回裸哨兵,
// body 不足返回 errors.WithStack 包装(生产 ltiv.go:75/86)。若漂移,
// processor 的错误分类路径将失去回归保护。
type ltivHelper struct{}

func (h *ltivHelper) Decode(data []byte) (uint64, *message.Message, error) {
	if len(data) < 4 {
		return 0, nil, packet.ErrPkgHeadNotEnough
	}
	size := binary.BigEndian.Uint32(data[:4])
	if len(data) < 4+int(size) {
		return 0, nil, errors.WithStack(packet.ErrPkgBodyNotEnough)
	}
	body := data[4 : 4+size]
	msg := &message.Message{}
	msg.Type = uint16(binary.BigEndian.Uint16(body[:2]))
	id := binary.BigEndian.Uint32(body[2:6])
	msg.Path = uint32ToString(id)
	msg.Payload = body[6:]
	return uint64(4 + size), msg, nil
}

func (h *ltivHelper) Encode(msg *message.Message) ([]byte, error) {
	buf := make([]byte, 0, 4+2+4+len(msg.Payload))
	size := 2 + 4 + len(msg.Payload)
	buf = appendUint32(buf, uint32(size))
	buf = appendUint16(buf, msg.Type)
	buf = appendUint32(buf, stringToUint32(msg.Path))
	buf = append(buf, msg.Payload...)
	return buf, nil
}

func appendUint16(b []byte, v uint16) []byte {
	return append(b, byte(v>>8), byte(v))
}

func appendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func uint32ToString(v uint32) string {
	if v == 0 {
		return ""
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func stringToUint32(s string) uint32 {
	var v uint32
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + uint32(c-'0')
	}
	return v
}

func TestProcessor_EncodeDecode_RoundTrip(t *testing.T) {
	p := newTestProcessor()
	req := &pb.NotifyMailUpdate{MailId: 12345}

	data, err := p.Encode(&message.Message{Msg: req})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty encoded data")
	}

	consumed, decoded, err := p.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if consumed != uint64(len(data)) {
		t.Fatalf("expected consume %d, got %d", len(data), consumed)
	}
	if decoded.Msg == nil {
		t.Fatal("decoded Msg is nil")
	}
	got, ok := decoded.Msg.(*pb.NotifyMailUpdate)
	if !ok {
		t.Fatalf("expected *pb.NotifyMailUpdate, got %T", decoded.Msg)
	}
	if got.MailId != 12345 {
		t.Fatalf("expected MailId 12345, got %d", got.MailId)
	}
}

func TestProcessor_Decode_UnknownPath(t *testing.T) {
	p := newTestProcessor()
	// 未注册的消息路径(999999 无对应 meta)
	data := make([]byte, 0, 4+2+4)
	data = appendUint32(data, 6)
	data = appendUint16(data, 1)
	data = appendUint32(data, 999999)

	_, _, err := p.Decode(data)
	if err == nil {
		t.Fatal("expected error for unknown message path")
	}
}

func TestProcessor_Decode_ShortPacket(t *testing.T) {
	p := newTestProcessor()
	// 数据不足 head → 返回 (0, nil, nil),不算错误
	consumed, msg, err := p.Decode([]byte{0x01})
	if err != nil {
		t.Fatalf("short packet should not error, got %v", err)
	}
	if consumed != 0 || msg != nil {
		t.Fatalf("expected (0, nil), got (%d, %+v)", consumed, msg)
	}
}

func TestProcessor_Decode_HalfPacket(t *testing.T) {
	// 回归:生产 ltiv 对 body 不足返回 errors.WithStack 包装,
	// processor 必须用 errors.Is 分类,否则半包触发 nil 解引用 panic(可被远程触发)。
	p := newTestProcessor()
	// 声明 size=100,实际只有 8 字节 body
	data := make([]byte, 4+8)
	binary.BigEndian.PutUint32(data[:4], 100)

	consumed, msg, err := p.Decode(data)
	if err != nil {
		t.Fatalf("half packet should not error, got %v", err)
	}
	if consumed != 0 || msg != nil {
		t.Fatalf("expected (0, nil), got (%d, %+v)", consumed, msg)
	}
}
