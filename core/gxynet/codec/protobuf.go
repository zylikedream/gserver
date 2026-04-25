package codec

import (
	"strconv"

	pb "gserver/protocol/pb"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

type ProtoBuf struct {
}

func newProtobuf() (*ProtoBuf, error) {
	return &ProtoBuf{}, nil
}

func (p *ProtoBuf) Decode(msg interface{}, data []byte) error {
	return proto.Unmarshal(data, msg.(proto.Message))
}

func (p *ProtoBuf) Encode(raw interface{}) ([]byte, error) {
	return proto.Marshal(raw.(proto.Message))
}

func init() {
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.FullName() == "galaxy.protocol" {
			msgs := fd.Messages()
			simpleMgs := []protoreflect.MessageDescriptor{}
			nestedMgs := []protoreflect.MessageDescriptor{}
			for i := 0; i < msgs.Len(); i++ {
				msgDesc := msgs.Get(i)
				neseted := false
				for i := 0; i < msgDesc.Fields().Len(); i++ {
					field := msgDesc.Fields().Get(i)
					fieldType := field.Kind().String()
					if fieldType == protoreflect.MessageKind.String() {
						neseted = true
						break
					}
				}
				if !neseted {
					simpleMgs = append(simpleMgs, msgDesc)
				} else {
					nestedMgs = append(nestedMgs, msgDesc)
				}
			}
			for _, msgDesc := range append(simpleMgs, nestedMgs...) {
				msgType, err := protoregistry.GlobalTypes.FindMessageByName(msgDesc.FullName())
				if err != nil {
					continue
				}
				msgIns := msgType.New().Interface()

				// 读取 msg_id option，如果有则用数字 ID 注册
				opts := msgDesc.Options()
				if opts != nil {
					if id := proto.GetExtension(opts, pb.E_MsgId).(uint32); id > 0 {
						RegisterMessageMeta(strconv.FormatUint(uint64(id), 10), msgIns)
						continue
					}
				}
				// 没有 msg_id 的消息（如服务端内部消息），用类型名注册
				RegisterMessageMeta(string(msgDesc.Name()), msgIns)
			}
		}
		return true
	})
}
