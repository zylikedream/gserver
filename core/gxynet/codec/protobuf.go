package codec

import (
	"google.golang.org/protobuf/proto"
)

type ProtoBuf struct {
}

func newProtobuf() (*ProtoBuf, error) {
	return &ProtoBuf{}, nil
}

func (p *ProtoBuf) Decode(msg interface{}, data []byte) error {
	// 处理包装器类型
	return proto.Unmarshal(data, msg.(proto.Message))
}

func (p *ProtoBuf) Encode(raw interface{}) ([]byte, error) {
	// 处理包装器类型
	// 处理原生proto消息
	return proto.Marshal(raw.(proto.Message))
}
