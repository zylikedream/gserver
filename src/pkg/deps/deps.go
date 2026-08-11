// Package deps 定义进程级依赖容器。
//
// 目标:消灭业务代码对全局单例(gxypgx.DB/gxyredis.Redis/gameconfig.Get)
// 的隐式访问,改为显式注入,使业务逻辑可测试(mock 注入)。
//
// Deps 由组装根(模块启动处)创建并注入业务模块;测试中构造自定义 Deps
// 传入 mock 依赖。
package deps

import (
	"gserver/core/gxyredis"
	"gserver/src/pkg/gameconfig"

	"gorm.io/gorm"
)

// Deps 业务模块所需的共享依赖。
type Deps struct {
	// DB 数据库连接(gorm)。测试注入 go-sqlmock 的 *gorm.DB。
	DB *gorm.DB
	// Redis 缓存客户端。测试注入 miniredis 客户端。
	Redis gxyredis.Client
	// Cfg 游戏配表(进程内只读单例)。测试注入自定义实例。
	Cfg *gameconfig.GameConfig
}
