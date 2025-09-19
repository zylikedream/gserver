package processor

import (
	"context"

	"gserver/core/gxynet/message"
	"gserver/core/gxynet/packet"
	"gserver/util"

	"github.com/gogf/gf/v2/os/gcfg"
)

var ctx = context.Background()

type Processor interface {
	Decode(data []byte) (uint64, *message.Message, error)
	Encode(msg *message.Message) ([]byte, error)
}

type processorConfig struct {
	PacketCodecType  string `toml:"packet"`
	MessageCodecType string `toml:"message"`
}

type processor struct {
	pktCodec packet.PacketCodec
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
	return proc, nil
}

func (p *processor) Decode(data []byte) (uint64, *message.Message, error) {
	pkgLen, msg, err := p.pktCodec.Decode(data)
	if err == packet.ErrPkgBodyNotEnough || err == packet.ErrPkgHeadNotEnough { // 数据不足够，不算错误
		return 0, nil, nil
	}
	return pkgLen, msg, err
}

func (p *processor) Encode(msg *message.Message) ([]byte, error) {
	data, err := p.pktCodec.Encode(msg)
	if err != nil {
		return nil, err
	}
	return data, err

}
