package processor

import (
	"context"
	"errors"

	"gserver/core/gxynet/codec"
	"gserver/core/gxynet/message"
	"gserver/core/gxynet/packet"
	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gcfg"
)

var ctx = context.Background()

type Processor interface {
	Decode(data []byte) (uint64, *message.Message, error)
	Encode(msg *message.Message) ([]byte, error)
}

type processorConfig struct {
	PacketCodecType  string `toml:"packet"`
	MessageCodecType string `toml:"codec"`
}

type processor struct {
	pktCodec packet.PacketCodec
	msgCodec codec.IMessageCodec
	conf     *processorConfig
}

func NewProcessor(c *gcfg.Config) (Processor, error) {
	proc := &processor{}
	conf := &processorConfig{}
	var err error
	if err = gxyutil.CfgUnmarshalKey(ctx, c, "processor", conf); err != nil {
		return nil, err
	}
	proc.conf = conf
	proc.pktCodec, err = packet.NewPacketCodec(conf.PacketCodecType, c)
	if err != nil {
		return nil, err
	}
	proc.msgCodec, err = codec.NewMessageCodec(conf.MessageCodecType)
	if err != nil {
		return nil, err
	}
	return proc, nil
}

func (p *processor) Decode(data []byte) (uint64, *message.Message, error) {
	pkgLen, msg, err := p.pktCodec.Decode(data)
	// 数据不足不算错误。生产 codec 对 body 不足返回 errors.WithStack 包装,
	// 必须用 errors.Is 而非 == 比较,否则半包/超大包会落到下面 nil 解引用。
	if errors.Is(err, packet.ErrPkgBodyNotEnough) || errors.Is(err, packet.ErrPkgHeadNotEnough) {
		return 0, nil, nil
	}
	meta := codec.MessageMetaByID(msg.Path)
	if meta == nil {
		meta = codec.MessageMetaByName(msg.Path)
	}
	if meta == nil {
		return 0, nil, gerror.Newf("message meta not found: %s", msg.Path)
	}
	msg.Msg = meta.NewInstance()
	if err = p.msgCodec.Decode(msg.Msg, msg.Payload); err != nil {
		return 0, nil, err
	}
	// 清空payload，避免占用内存
	msg.Payload = []byte{}
	return pkgLen, msg, err
}

func (p *processor) Encode(msg *message.Message) ([]byte, error) {
	if len(msg.Payload) == 0 {
		msg.Path = codec.MessageMetaByMsg(msg.Msg).ID
		msg.Type = message.MESSAGE_TYPE_DATA_PACKET
		var err error
		msg.Payload, err = p.msgCodec.Encode(msg.Msg)
		if err != nil {
			return nil, err
		}
	}
	data, err := p.pktCodec.Encode(msg)
	if err != nil {
		return nil, err
	}
	return data, err

}
