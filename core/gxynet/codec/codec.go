package codec

import (
	"github.com/gogf/gf/v2/errors/gerror"
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
	default:
		return nil, gerror.Newf("message codec %s not register", t)
	}
}
