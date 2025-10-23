package util

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"

	"github.com/go-viper/mapstructure/v2"
	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/os/gfile"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gookit/goutil/reflects"
	"github.com/mitchellh/hashstructure/v2"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func GetName[T any]() string {
	return GetTypeName(reflect.TypeFor[T]())
}

func GetObjectName(obj interface{}) string {
	return GetTypeName(reflect.TypeOf(obj))
}

func GetTypeName(t reflect.Type) string {
	return reflects.TypeReal(t).Name()
}

func NewObject(t reflect.Type) interface{} {
	return reflect.New(reflects.TypeReal(t)).Interface()
}

func GetObjectHash(obj interface{}) uint64 {
	hash, err := hashstructure.Hash(obj, hashstructure.FormatV2, nil)
	if err != nil {
		glog.Error(context.Background(), "hash object err", zap.Any("obj", obj), zap.Error(err))
		return 0
	}
	return hash
}

func CfgToViper(ctx context.Context, c *gcfg.Config) *viper.Viper {
	vp := viper.New()
	vp.MergeConfigMap(c.MustData(ctx))
	return vp
}

func CfgUnmarshal(ctx context.Context, c *gcfg.Config, data any) error {
	vp := CfgToViper(ctx, c)
	return vp.Unmarshal(data)
}

func CfgUnmarshalKey(ctx context.Context, c *gcfg.Config, key string, data any) error {
	adapter, ok := c.GetAdapter().(*gcfg.AdapterFile)
	var opt viper.DecoderConfigOption = nil
	if ok {
		opt = func(c *mapstructure.DecoderConfig) {
			c.TagName = gfile.ExtName(adapter.GetFileName())
		}
	}
	vp := CfgToViper(ctx, c)

	return vp.UnmarshalKey(key, data, opt)
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
	return fmt.Sprintf("%T%s", msg, gjson.MustEncode(msg))
}
