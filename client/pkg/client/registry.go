package client

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	pb "hy_client/pb"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var (
	metaByID   = map[string]*messageMeta{}
	metaByType = map[reflect.Type]*messageMeta{}
)

type messageMeta struct {
	id   string
	name string
	typ  reflect.Type
	md   protoreflect.MessageDescriptor
}

var registered bool

func RegisterMessages() {
	if registered {
		return
	}
	registered = true

	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "galaxy.protocol" {
			return true
		}
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			registerMessage(msgs.Get(i))
		}
		return true
	})
}

func registerMessage(md protoreflect.MessageDescriptor) {
	opts := md.Options()
	if opts == nil {
		return
	}
	msgID := proto.GetExtension(opts, pb.E_MsgId).(uint32)
	if msgID == 0 {
		return
	}

	msgType, err := protoregistry.GlobalTypes.FindMessageByName(md.FullName())
	if err != nil {
		return
	}
	instance := msgType.New().Interface()

	id := strconv.FormatUint(uint64(msgID), 10)
	typ := reflect.TypeOf(instance)
	typ = typ.Elem()

	meta := &messageMeta{
		id:   id,
		name: string(md.Name()),
		typ:  typ,
		md:   md,
	}
	metaByID[id] = meta
	metaByType[typ] = meta
}

func MessageTypeByID(id string) reflect.Type {
	m, ok := metaByID[id]
	if !ok {
		return nil
	}
	return m.typ
}

func IDByMessage(msg proto.Message) string {
	typ := reflect.TypeOf(msg).Elem()
	m, ok := metaByType[typ]
	if !ok {
		return ""
	}
	return m.id
}

func NewMessageByID(id string) proto.Message {
	m, ok := metaByID[id]
	if !ok {
		return nil
	}
	val := reflect.New(m.typ)
	msg, ok := val.Interface().(proto.Message)
	if !ok {
		return nil
	}
	return msg
}

func NewMessageByName(name string) proto.Message {
	mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName("galaxy.protocol." + name))
	if err != nil {
		return nil
	}
	return mt.New().Interface()
}

func MessageNameByID(id string) string {
	m, ok := metaByID[id]
	if !ok {
		return fmt.Sprintf("Unknown(%s)", id)
	}
	return m.name
}

func RangeReqMessages(fn func(msgID, name string, md protoreflect.MessageDescriptor) bool) {
	for id, meta := range metaByID {
		if !strings.HasPrefix(meta.name, "Req") {
			continue
		}
		if !fn(id, meta.name, meta.md) {
			break
		}
	}
}
