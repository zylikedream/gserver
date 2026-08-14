package packet

import (
	"encoding/binary"
	stderrors "errors"
	"fmt"

	"gserver/core/gxynet/message"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gcfg"
)

// 哨兵错误:标准库 errors.New(无栈),使用处用 errors.WithStack 补栈指向实际出错点。
var (
	ErrPkgHeadNotEnough = stderrors.New("pkg header not enougth")
	ErrPkgBodyNotEnough = stderrors.New("pkg body not enougth")
)

type PacketCodec interface {
	Decode(data []byte) (uint64, *message.Message, error) // 解析包体
	Encode(msg *message.Message) ([]byte, error)          // 打包消息
	Type() string
}

func convertUint(v uint64, len int) interface{} {
	switch len {
	case 1:
		return uint8(v)
	case 2:
		return uint16(v)
	case 4:
		return uint32(v)
	case 8:
		return v
	}
	return v
}

func uintDecode(data []byte, byteOrder binary.ByteOrder) (uint64, error) {
	switch len(data) {
	case 1:
		return uint64(data[0]), nil
	case 2:
		return uint64(byteOrder.Uint16(data)), nil
	case 4:
		return uint64(byteOrder.Uint32(data)), nil
	case 8:
		return uint64(byteOrder.Uint64(data)), nil
	}
	return 0, fmt.Errorf("unsupport byte len:%d", len(data))
}

const (
	PACKET_LTIV = "packet.ltiv"
	PACKET_LTPV = "packet.ltpv"
)

func NewPacketCodec(t string, c *gcfg.Config) (PacketCodec, error) {
	switch "packet." + t {
	case PACKET_LTIV:
		return newLtiv(c)
	case PACKET_LTPV:
		return newLtpv(c)
	}
	return nil, gerror.Newf("packet type:%s not exists", t)
}
