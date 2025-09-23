package codec

import (
	"context"

	"ergo.services/ergo/net/edf"
	"github.com/gogf/gf/v2/os/glog"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// ProtoMessageWrapper 是一个包装类型，用于为所有Protocol Buffers消息提供统一的EDF序列化支持
type ProtoMessageWrapper struct {
	Message proto.Message
}

// NewProtoMessageWrapper 创建一个新的Protocol Buffers消息包装器
func NewProtoMessageWrapper(msg proto.Message) *ProtoMessageWrapper {
	return &ProtoMessageWrapper{
		Message: msg,
	}
}

// MarshalEDF 实现edf.Marshaler接口，使用Protocol Buffers的序列化机制
func (w *ProtoMessageWrapper) MarshalEDF() ([]byte, error) {
	if w.Message == nil {
		return nil, nil
	}
	return proto.Marshal(w.Message)
}

// UnmarshalEDF 实现edf.Unmarshaler接口，使用Protocol Buffers的反序列化机制
func (w *ProtoMessageWrapper) UnmarshalEDF(data []byte) error {
	if w.Message == nil {
		return nil
	}
	return proto.Unmarshal(data, w.Message)
}

// 更新初始化函数，使用包装器来处理Protocol Buffers消息的EDF注册
func init() {
	// 遍历protofile，使用edf.RegisterType进行注册
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		glog.Infof(context.Background(), "RegisterTypeOf %s, file: %s %s", fd.Path(), fd.FullName(), fd.Name())
		if fd.FullName() == "galaxy.protocol" {
			// 只保留basic.proto这种鸡蛋
			msgs := fd.Messages()
			simpleMgs := []protoreflect.MessageDescriptor{}
			nestedMgs := []protoreflect.MessageDescriptor{}
			for i := 0; i < msgs.Len(); i++ {
				msgDesc := msgs.Get(i)
				neseted := false
				for i := 0; i < msgDesc.Fields().Len(); i++ {
					field := msgDesc.Fields().Get(i)
					fieldType := field.Kind().String()
					if fieldType == protoreflect.MessageKind.String() { // 嵌套类型
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
				glog.Infof(context.Background(), "RegisterMessageOf %s", msgDesc.FullName())
				msgType, err := protoregistry.GlobalTypes.FindMessageByName(msgDesc.FullName())
				if err != nil {
					continue
				}
				msgIns := msgType.New().Interface()
				RegisterMessageMeta(string(msgDesc.Name()), msgIns)

				// 不再直接对原始消息类型调用edf.RegisterTypeOf，而是使用包装器
				// 但是仍然需要注册一个示例，以便EDF可以识别这种类型
				// 我们可以使用包装器的类型进行注册
				wrapper := NewProtoMessageWrapper(msgIns)
				if err = edf.RegisterTypeOf(wrapper); err != nil {
					glog.Infof(context.Background(), "RegisterTypeOf wrapper for %s failed: %v, will try other method", msgDesc.Name(), err)
				}
			}
		}
		return true
	})
}
