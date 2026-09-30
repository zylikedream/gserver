package uid

import (
	"fmt"

	"github.com/gogf/gf/v2/util/guid"
	"gorm.io/gorm"
)

const (
	UID_GROUP_PREFIX = "uid"
)

// Gen 从 PostgreSQL sequence 取全局自增 id。
// 持久、原子、多实例安全;sequence 命名约定: uid_<group>_seq(如 uid_role_seq)。
// nextval 一旦消耗不回退(事务回滚/失败会跳号,调用方需容忍空洞)。
//
// db 由组装根注入(见 invariants.md「测试替身规则」);测试注入 sqlmock。
type Gen struct {
	db *gorm.DB
}

func NewGen(db *gorm.DB) *Gen {
	return &Gen{db: db}
}

func (g *Gen) GenAutoIncID(Group string) (int64, error) {
	if g.db == nil {
		return 0, fmt.Errorf("uid gen: db not initialized")
	}
	seq := fmt.Sprintf("%s_%s_seq", UID_GROUP_PREFIX, Group)
	var id int64
	if err := g.db.Raw("SELECT nextval(?::regclass)", seq).Scan(&id).Error; err != nil {
		return 0, err
	}
	return id, nil
}

// RandomStrID 生成随机字符串 id(不依赖外部资源)。
func RandomStrID() string {
	return guid.S()
}
