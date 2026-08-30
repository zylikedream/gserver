package uid

import (
	"fmt"

	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/util/guid"
)

type uidGen struct {
}

const (
	UID_GROUP_PREFIX = "uid"
)

// uidDB 可替换函数变量:测试注入 go-sqlmock(编译期安全,非 gomonkey;ADR-0001)。
var uidDB = gxypgx.DB

var defaultUidGen *uidGen

func UidGen() *uidGen {
	return defaultUidGen
}

func init() {
	defaultUidGen = &uidGen{}
}

// GenAutoIncID 从 PostgreSQL sequence 取全局自增 id。
// 持久、原子、多实例安全;sequence 命名约定: uid_<group>_seq(如 uid_role_seq)。
// nextval 一旦消耗不回退(事务回滚/失败会跳号,调用方需容忍空洞)。
func (u *uidGen) GenAutoIncID(Group string) (int64, error) {
	db := uidDB()
	if db == nil {
		return 0, fmt.Errorf("uid gen: db not initialized")
	}
	seq := fmt.Sprintf("%s_%s_seq", UID_GROUP_PREFIX, Group)
	var id int64
	if err := db.Raw("SELECT nextval(?::regclass)", seq).Scan(&id).Error; err != nil {
		return 0, err
	}
	return id, nil
}

func (u *uidGen) GenRandomStrID() string {
	return guid.S()
}
