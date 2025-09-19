package proto

import (
	"hash/crc32"

	"gserver/core/gxynet/message"

	"github.com/gogf/gf/v2/util/gconv"
)

func init() {
	message.RegisterMessageMeta(gconv.String(crc32.ChecksumIEEE([]byte("EchoReq"))), (*EchoReq)(nil))
	message.RegisterMessageMeta(gconv.String(crc32.ChecksumIEEE([]byte("EchoAck"))), (*EchoAck)(nil))
}
