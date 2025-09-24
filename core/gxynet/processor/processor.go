package processor

import (
	"context"

	"gserver/core/gxynet/codec"
	"gserver/core/gxynet/message"
	"gserver/core/gxynet/packet"
	"gserver/util"

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
	if err = util.CfgUnmarshalKey(ctx, c, "processor", conf); err != nil {
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
	if err == packet.ErrPkgBodyNotEnough || err == packet.ErrPkgHeadNotEnough { // 数据不足够，不算错误
		return 0, nil, nil
	}
	meta := codec.MessageMetaByName(msg.Path)
	if meta == nil {
		return 0, nil, gerror.Newf("message meta not found: %s", msg.Path)
	}
	msg.Msg = codec.MessageMetaByName(msg.Path).NewInstance()
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
