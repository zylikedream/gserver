package uid

import (
	"context"
	"fmt"

	"gserver/core/gxyredis"

	"github.com/gogf/gf/v2/util/guid"
)

type uidGen struct {
}

const (
	UID_GROUP_PREFIX = "uid"
)

var defaultUidGen *uidGen

func UidGen() *uidGen {
	return defaultUidGen
}

func init() {
	defaultUidGen = &uidGen{}
}

func (u *uidGen) GenAutoIncID(Group string) (int64, error) {
	redis := gxyredis.GetRedis()
	key := fmt.Sprintf("%s.%s", UID_GROUP_PREFIX, Group)
	return redis.Incr(context.Background(), key)
}

func (u *uidGen) GenRandomStrID() string {
	return guid.S()
}
