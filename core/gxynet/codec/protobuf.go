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

func (p *ProtoBuf) Decode(msg any, data []byte) error {
	return proto.Unmarshal(data, msg.(proto.Message))
}

func (p *ProtoBuf) Encode(raw any) ([]byte, error) {
	return proto.Marshal(raw.(proto.Message))
}

func init() {
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.FullName() == "galaxy.protocol" {
			registerGalaxyMessages(fd.Messages())
		}
		return true
	})
}

// registerGalaxyMessages 注册 galaxy.protocol 中的消息:无嵌套的先注册,再注册嵌套的。
func registerGalaxyMessages(msgs protoreflect.MessageDescriptors) {
	simple, nested := classifyMessages(msgs)
	for _, msgDesc := range append(simple, nested...) {
		registerMessage(msgDesc)
	}
}

// classifyMessages 按是否含消息类型字段,把消息分成"简单"与"嵌套"两组。
func classifyMessages(msgs protoreflect.MessageDescriptors) (simple, nested []protoreflect.MessageDescriptor) {
	for i := 0; i < msgs.Len(); i++ {
		msgDesc := msgs.Get(i)
		if hasMessageField(msgDesc) {
			nested = append(nested, msgDesc)
		} else {
			simple = append(simple, msgDesc)
		}
	}
	return simple, nested
}

// hasMessageField 报告消息是否含嵌套消息类型字段。
func hasMessageField(msgDesc protoreflect.MessageDescriptor) bool {
	fields := msgDesc.Fields()
	for i := 0; i < fields.Len(); i++ {
		if fields.Get(i).Kind() == protoreflect.MessageKind {
			return true
		}
	}
	return false
}

// registerMessage 注册单条消息:有 msg_id 用数字 ID,否则用类型名。
func registerMessage(msgDesc protoreflect.MessageDescriptor) {
	msgType, err := protoregistry.GlobalTypes.FindMessageByName(msgDesc.FullName())
	if err != nil {
		return
	}
	msgIns := msgType.New().Interface()
	if id := messageIDOption(msgDesc); id > 0 {
		RegisterMessageMeta(strconv.FormatUint(uint64(id), 10), msgIns)
		return
	}
	RegisterMessageMeta(string(msgDesc.Name()), msgIns)
}

// messageIDOption 读取消息的 msg_id option;没有则返回 0。
func messageIDOption(msgDesc protoreflect.MessageDescriptor) uint32 {
	opts := msgDesc.Options()
	if opts == nil {
		return 0
	}
	return proto.GetExtension(opts, pb.E_MsgId).(uint32)
}
