package gxyutil

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/cockroachdb/errors"
	"github.com/go-viper/mapstructure/v2"
	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/gfile"
	"github.com/gogo/protobuf/proto"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/encoding/prototext"
	googleProto "google.golang.org/protobuf/proto"
)

func CfgToViper(ctx context.Context, c *gcfg.Config) *viper.Viper {
	vp := viper.New()
	_ = vp.MergeConfigMap(c.MustData(ctx))
	return vp
}

func CfgUnmarshal(ctx context.Context, c *gcfg.Config, data any) error {
	vp := CfgToViper(ctx, c)
	return vp.Unmarshal(data, cfgDecoderConfigOption(c))
}
func CfgUnmarshalKey(ctx context.Context, c *gcfg.Config, key string, data any) error {
	vp := CfgToViper(ctx, c)
	return vp.UnmarshalKey(key, data, cfgDecoderConfigOption(c))
}

// CfgUnmarshalKeyExact 将配置中指定 key 的子节严格解码到 out:
//   - 子节缺失时返回带堆栈的 cockroachdb/errors 错误;
//   - 未知字段报错(UnmarshalExact), 用于启动期严格校验。
//
// 非文件适配器(如测试用内存适配器)没有文件扩展名可作 TagName,
// 此时退回 mapstructure 默认的字段名匹配。
func CfgUnmarshalKeyExact(ctx context.Context, c *gcfg.Config, key string, out any) error {
	vp := CfgToViper(ctx, c)
	sub := vp.Sub(key)
	if sub == nil {
		return errors.Newf("config section %q is missing", key)
	}
	if opt := cfgDecoderConfigOption(c); opt != nil {
		return sub.UnmarshalExact(out, opt)
	}
	return sub.UnmarshalExact(out)
}

func cfgDecoderConfigOption(c *gcfg.Config) viper.DecoderConfigOption {
	adapter, ok := c.GetAdapter().(*gcfg.AdapterFile)
	if !ok {
		return nil
	}
	return func(c *mapstructure.DecoderConfig) {
		c.TagName = gfile.ExtName(adapter.GetFileName())
	}
}

func GetCurrenDir() string {
	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		return ""
	}
	return filepath.Dir(filename)
}

func FormatObject(msg any) string {
	if msg == nil {
		return "nil{}"
	}
	if pbMsg, ok := msg.(googleProto.Message); ok {
		return fmt.Sprintf("%T{%s}", msg, prototext.MarshalOptions{Multiline: false}.Format(pbMsg))
	}
	return fmt.Sprintf("%T%s", msg, gjson.MustEncode(msg))
}

func EncodeMsg(msg any) ([]byte, error) {
	switch v := msg.(type) {
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	case proto.Message:
		return proto.Marshal(v)
	default:
		return gjson.Encode(v)
	}
}

func DecodeMsg(msgData []byte, msg any) error {
	switch v := msg.(type) {
	case *string:
		*v = string(msgData)
		return nil
	case *[]byte:
		*v = msgData
		return nil
	case proto.Message:
		return proto.Unmarshal(msgData, v)
	default:
		return gjson.Unmarshal(msgData, v)
	}
}

func SetConfig(config string) {
	g.Cfg().GetAdapter().(*gcfg.AdapterFile).SetFileName(config)
}

func If[T any](condition bool, trueValue, falseValue T) T {
	if condition {
		return trueValue
	}
	return falseValue
}
