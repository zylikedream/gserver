package message

import (
	"context"

	"gserver/core/gxymodule"
	"gserver/util"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gcfg"
)

const (
	MESSGE_TYPE_FIRST_PACKET = iota
	MESSAGE_TYPE_DATA_PACKET
)

type IEncoder interface {
	Encode(v any) ([]byte, error)
}

type IDecoder interface {
	Decode(msg interface{}, data []byte) error
}

type IMessageCodec interface {
	IEncoder
	IDecoder
}

type Message struct {
	Path    string
	Type    uint16
	Payload []byte
}

type messageOption struct {
	packType uint16
	codec    IMessageCodec
}

type MessageOptionFunc func(m *messageOption)

func WithType(t int) MessageOptionFunc {
	return func(m *messageOption) {
		m.packType = uint16(t)
	}
}

func NewMessageRaw(data []byte, path string, opts ...MessageOptionFunc) (*Message, error) {
	opt := &messageOption{
		packType: MESSAGE_TYPE_DATA_PACKET,
	}
	for _, optFunc := range opts {
		optFunc(opt)
	}
	msg := &Message{
		Path:    path,
		Payload: data,
		Type:    opt.packType,
	}
	return msg, nil
}

func NewNetMessage(ins any, opts ...MessageOptionFunc) (*Message, error) {
	return NewMessage(ins, netcodec, opts...)
}

func NewMessage(ins any, encoder IEncoder, opts ...MessageOptionFunc) (*Message, error) {
	opt := &messageOption{
		packType: MESSAGE_TYPE_DATA_PACKET,
	}
	for _, optFunc := range opts {
		optFunc(opt)
	}
	data, err := encoder.Encode(ins)
	if err != nil {
		return nil, err
	}
	path := util.GetObjectName(ins)
	message := &Message{
		Path:    path,
		Payload: data,
		Type:    opt.packType,
	}
	return message, nil
}

const (
	MESSAGE_JSON     = "json"
	MESSAGE_PROTOBUF = "protobuf"
	MESSAGE_MSGPACK  = "msgpack"
	MESSAGE_RAW      = "raw"
)

func NewMessageCodec(t string) (IMessageCodec, error) {
	switch t {
	case MESSAGE_JSON:
		return newJsonMessage()
	case MESSAGE_PROTOBUF:
		return newProtobuf()
	case MESSAGE_RAW:
		return newRawMessage()
	default:
		return nil, gerror.Newf("message codec %s not register", t)
	}
}

type NetCodec struct {
	IMessageCodec
	gxymodule.Module
	config string
}

var netcodec *NetCodec

func GetNetCodec() *NetCodec {
	return netcodec
}

func NewNetCodec(config string) *NetCodec {
	netcodec = &NetCodec{
		config: config,
	}
	return netcodec
}
func (m *NetCodec) OnInit(ctx context.Context) error {
	cfg := gcfg.Instance(m.config)
	t := cfg.MustGet(context.Background(), "message.codec").String()
	var err error
	m.IMessageCodec, err = NewMessageCodec(t)
	if err != nil {
		return err
	}
	return nil
}
