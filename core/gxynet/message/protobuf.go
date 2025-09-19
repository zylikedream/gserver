package message

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type ProtoBuf struct {
}

func newProtobuf() (*ProtoBuf, error) {
	// protoPath := c.MustGet(context.Background(), fmt.Sprintf("%s.%s", MESSAGE_PROTOBUF, "proto_path")).String()
	// RegisterProtoFiles(protoPath)
	return &ProtoBuf{}, nil
}

func (p *ProtoBuf) Decode(msg interface{}, data []byte) error {
	return proto.Unmarshal(data, msg.(protoreflect.ProtoMessage))
}

func (p *ProtoBuf) Encode(raw interface{}) ([]byte, error) {
	data, err := proto.Marshal(raw.(proto.Message))
	if err != nil {
		return nil, err
	}
	return data, nil
}
