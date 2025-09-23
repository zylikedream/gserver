package codec

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type ProtoBuf struct {
}

func newProtobuf() (*ProtoBuf, error) {
	return &ProtoBuf{}, nil
}

func (p *ProtoBuf) Decode(msg interface{}, data []byte) error {
	// 处理包装器类型
	if wrapper, ok := msg.(*ProtoMessageWrapper); ok {
		return proto.Unmarshal(data, wrapper.Message)
	}
	// 处理原生proto消息
	return proto.Unmarshal(data, msg.(protoreflect.ProtoMessage))
}

func (p *ProtoBuf) Encode(raw interface{}) ([]byte, error) {
	// 处理包装器类型
	if wrapper, ok := raw.(*ProtoMessageWrapper); ok {
		return proto.Marshal(wrapper.Message)
	}
	// 处理原生proto消息
	return proto.Marshal(raw.(proto.Message))
}
