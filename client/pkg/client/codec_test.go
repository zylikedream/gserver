package client

import (
	"testing"
)

// TestEncodeDecodeHandshake:逐字节钉死 LTIV 定长头布局——
// Size(2B, 小端, **不含自身**) + Type(1B) + ID(2B, 小端) = 5 字节,其后才是 Payload。
// 对应生产 LTIVCodec.Encode 里的 body = Type(1B)+ID(2B) 与 packet = Size(2B)+body。
// 下面 expectedSize 里的 `1 + 2` 就是 Type(1B)+ID(2B),若头里增删字段这个常数必须同步改。
// 为什么需要:服务端(Go)与跨语言客户端按同一布局解析,改动头布局会静默打断互通——
// 单侧测试不会发现,要到联调才暴露。Type 用 0 表示握手。
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

// TestDecodePartialData:半包必须按**头部不够 / 体不够**两种情况分别报错,且用 `!=` 与哨兵
// 比较——这同时钉死了哨兵是**原样返回、不带包装**的(errors.New 的 identity,不能是 fmt.Errorf("%w") 包过的)。
// 为什么需要:conn.readLoop 的内层循环只凭 `err != nil` 判断"这批字节还不完整"并原样保留 c.buf
// 等下一次读;半包若被当成完整包(或错误被吞掉),粘包边界错位,后续每一条消息都解析成垃圾。
// 对外调用方按 == / errors.Is 区分这两个哨兵,包一层就再也匹配不上。
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

// TestEncodeDecodeDataPacket:握手(Type=0)的孪生用例,唯一差别是 Type 字节写 1,证明同一段
// 编解码路径对业务数据帧同样成立;布局本身见 TestEncodeDecodeHandshake,这里不重复。
// 往返证明 Path 以 uint16 过线后还能还原成 "21001",且 consumed == len(data) 意味着缓冲区被
// 一次读净——残留字节会让 readLoop 把下一帧当成这一帧的尾巴。
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
