package gxyactor

import (
	"gserver/core/gxynet/codec"

	"github.com/cockroachdb/errors"
	"google.golang.org/protobuf/proto"
)

// WireEnvelope 是跨节点消息的统一承载。
//
// 业务消息是 protobuf 生成类型,含未导出字段,运行时无法直接序列化它,
// 因此跨节点时统一装进本信封:信封只带一个消息标识与已序列化的载荷,
// 运行时只需认识这一个类型。
//
// 本机投递不经信封(零拷贝),因此信封只出现在真正跨节点的路径上。
type WireEnvelope struct {
	// MsgID 取自协议注册表(数字消息号或内部消息类型名)。
	MsgID string
	// Data 是 proto.Marshal 的结果。
	Data []byte
}

// packForWire 把消息装进信封。仅对 protobuf 消息生效。
// 返回 nil 表示该消息不能跨节点传输。
func packForWire(message any) (*WireEnvelope, error) {
	pbMsg, ok := message.(proto.Message)
	if !ok {
		return nil, errors.Newf("message %T is not a protobuf message and cannot cross nodes", message)
	}
	meta := codec.MessageMetaByMsg(pbMsg)
	if meta == nil {
		return nil, errors.Newf("message %T is not registered in the protocol registry", message)
	}
	data, err := proto.Marshal(pbMsg)
	if err != nil {
		return nil, errors.Wrapf(err, "marshal %T", message)
	}
	return &WireEnvelope{MsgID: meta.ID, Data: data}, nil
}

// UnwrapWire 还原信封内的消息。非信封原样返回。
// 业务在处理运行时回调参数时可能需要显式调用。
func UnwrapWire(message any) (any, error) {
	var env *WireEnvelope
	switch m := message.(type) {
	case *WireEnvelope:
		env = m
	case WireEnvelope:
		env = &m
	default:
		return message, nil
	}

	meta := codec.MessageMetaByID(env.MsgID)
	if meta == nil {
		return nil, errors.Newf("unknown message id %q", env.MsgID)
	}
	instance, ok := meta.NewInstance().(proto.Message)
	if !ok {
		return nil, errors.Newf("message id %q is not a protobuf message", env.MsgID)
	}
	if err := proto.Unmarshal(env.Data, instance); err != nil {
		return nil, errors.Wrapf(err, "unmarshal message id %q", env.MsgID)
	}
	return instance, nil
}
