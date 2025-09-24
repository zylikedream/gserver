package codec

import (
	"context"
	"encoding"
	"reflect"

	"ergo.services/ergo/net/edf"
	"github.com/gogf/gf/v2/os/glog"
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
	// 处理包装器类型
	return proto.Unmarshal(data, msg.(proto.Message))
}

func (p *ProtoBuf) Encode(raw interface{}) ([]byte, error) {
	// 处理包装器类型
	// 处理原生proto消息
	return proto.Marshal(raw.(proto.Message))
}

// 更新初始化函数，使用包装器来处理Protocol Buffers消息的EDF注册
func init() {
	// 遍历protofile，使用edf.RegisterType进行注册
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
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
				elem := reflect.ValueOf(msgIns).Elem().Interface()
				_, ok := msgIns.(encoding.BinaryMarshaler)
				glog.Infof(context.Background(), "%s is binary mshaler %s", msgDesc.Name(), ok)
				if err = edf.RegisterTypeOf(elem); err != nil {
					glog.Infof(context.Background(), "RegisterTypeOf for %s failed: %v", msgDesc.Name(), err)
				}
			}
		}
		return true
	})
}
