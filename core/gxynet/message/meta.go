package message

import (
	"context"
	"fmt"
	"os"
	"path"
	"reflect"
	"strings"

	"gserver/util"

	"github.com/gogf/gf/v2/os/glog"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var (
	// 消息元信息与消息名称，消息ID和消息类型的关联关系
	metaByFullName = map[string]*MessageMeta{}
	metaByID       = map[string]*MessageMeta{}
	metaByType     = map[reflect.Type]*MessageMeta{}
)

type MessageMeta struct {
	ID       string
	FullName string
	Type     reflect.Type
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

func RegisterMessageMeta(ID string, msg interface{}) *MessageMeta {
	meta := &MessageMeta{
		ID:   ID,
		Type: reflect.TypeOf(msg),
	}
	// 注册时, 统一为非指针类型
	if meta.Type.Kind() == reflect.Ptr {
		meta.Type = meta.Type.Elem()
	}
	meta.FullName = fullName(meta.Type)

	if _, ok := metaByType[meta.Type]; ok {
		panic(fmt.Sprintf("Duplicate message meta register by type: %s name: %s", meta.ID, meta.Type.Name()))
	} else {
		metaByType[meta.Type] = meta
	}

	if _, ok := metaByFullName[meta.FullName]; ok {
		panic(fmt.Sprintf("Duplicate message meta register by fullname: %s", meta.FullName))
	} else {
		metaByFullName[meta.FullName] = meta
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

func parseProtoFiles(protoPath string) []string {
	var ctx = context.Background()
	dir, _ := os.Getwd()
	glog.Info(ctx, "curdir = ", dir)
	pbfiles, err := os.ReadDir(protoPath)
	if err != nil {
		glog.Errorf(ctx, "read dir %s failed:%s", protoPath, err)
		return nil
	}
	pbFiles := []string{}
	for _, f := range pbfiles {
		if f.IsDir() {
			continue
		}
		if strings.HasSuffix(f.Name(), ".proto") {
			pbFiles = append(pbFiles, f.Name())
		}
	}
	return pbFiles
}

func RegisterProtoFiles(protoPath string) error {
	files := parseProtoFiles(protoPath)
	for _, pbName := range files {
		file, err := protoregistry.GlobalFiles.FindFileByPath(pbName)
		if err != nil {
			fmt.Printf("find pb error:%s filename:%s\n", err, pbName)
			continue
		}
		msgs := file.Messages()
		for i := 0; i < msgs.Len(); i++ {
			msg := msgs.Get(i)
			msgType, err := protoregistry.GlobalTypes.FindMessageByName(msg.FullName())
			if err != nil {
				return err
			}
			pbMsg := msgType.New().Interface()
			RegisterMessageMeta(string(msg.Name()), pbMsg)
		}
	}
	return nil
}

func MessageMetaByID(id string) *MessageMeta {
	if v, ok := metaByID[id]; ok {
		return v
	}

	return nil
}

func MessageMetaByType(t reflect.Type) *MessageMeta {

	if t == nil {
		return nil
	}

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if v, ok := metaByType[t]; ok {
		return v
	}

	return nil
}

// 根据消息对象获得消息元信息
func MessageMetaByMsg(msg interface{}) *MessageMeta {

	if msg == nil {
		return nil
	}

	return MessageMetaByType(reflect.TypeOf(msg))
}
