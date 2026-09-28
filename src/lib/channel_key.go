package lib

import (
	"fmt"
	"strconv"

	"github.com/cockroachdb/errors"
)

// ChannelKey 是频道 actor 的身份:频道类型 + 频道 ID。
//
// 这两者总是一起构成一个频道,并派生出 actor 的注册 id(形如 "type_id")。
// 散着传两个值时,拼 id 的格式在每个使用点各写一遍:拼错不会有编译期报错,
// 只会让频道注册到一个查不到的名字下。
type ChannelKey struct {
	Type int32
	ID   int64
}

// String 派生 actor 的注册 id,格式 "type_id"。
func (k ChannelKey) String() string {
	return strconv.Itoa(int(k.Type)) + "_" + strconv.FormatInt(k.ID, 10)
}

// ParseChannelKey 从注册 id 解析频道身份。
func ParseChannelKey(s string) (ChannelKey, error) {
	var k ChannelKey
	if _, err := fmt.Sscanf(s, "%d_%d", &k.Type, &k.ID); err != nil {
		return ChannelKey{}, errors.Wrapf(err, "invalid channel key %q", s)
	}
	return k, nil
}
