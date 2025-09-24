package codec

import (
	"fmt"
	"path"
	"reflect"
	"strings"

	"gserver/util"
)

var (
	// 消息元信息与消息名称，消息ID和消息类型的关联关系
	metaByName = map[string]*MessageMeta{}
	metaByID   = map[string]*MessageMeta{}
)

type MessageMeta struct {
	ID   string
	Name string
	Type reflect.Type
}

func fullName(t reflect.Type) string {

	var sb strings.Builder
	sb.WriteString(path.Base(t.PkgPath()))
	sb.WriteString(".")
	sb.WriteString(t.Name())

	return sb.String()
}

func (m *MessageMeta) TypeName() string {

	if m == nil {
		return ""
	}

	return m.Type.Name()
}

func (m *MessageMeta) NewInstance() interface{} {
	if m.Type == nil {
		return nil
	}
	return util.NewObject(m.Type)
}

func RegisterMessageMeta(ID string, msg any) *MessageMeta {
	meta := &MessageMeta{
		ID:   ID,
		Type: reflect.TypeOf(msg),
	}
	// 注册时, 统一为非指针类型
	if meta.Type.Kind() == reflect.Ptr {
		meta.Type = meta.Type.Elem()
	}
	meta.Name = meta.Type.Name()

	if _, ok := metaByName[meta.Name]; ok {
		panic(fmt.Sprintf("Duplicate message meta register by fullname: %s", meta.Name))
	} else {
		metaByName[meta.Name] = meta
	}

	if meta.ID == "" {
		panic("message meta require 'ID' field: " + meta.TypeName())
	}

	if prev, ok := metaByID[meta.ID]; ok {
		panic(fmt.Sprintf("Duplicate message meta register by id: %s type: %s, pre type: %s", meta.ID, meta.TypeName(), prev.TypeName()))
	} else {
		metaByID[meta.ID] = meta
	}

	return meta
}

func MessageMetaByID(id string) *MessageMeta {
	if v, ok := metaByID[id]; ok {
		return v
	}

	return nil
}

func MessageMetaByName(name string) *MessageMeta {
	if v, ok := metaByName[name]; ok {
		return v
	}

	return nil
}

// 根据消息对象获得消息元信息
func MessageMetaByMsg(msg interface{}) *MessageMeta {

	if msg == nil {
		return nil
	}

	return MessageMetaByName(fullName(reflect.TypeOf(msg)))
}
