package gxyregistery

import (
	"gserver/core/gxyregistery/types"
)

// HashServices 是某个服务名下的成员快照。
// Hash 由 updateServices 递增生成,消费方(选择器)据此判断成员是否变化。
type HashServices struct {
	ServiceInfos []*types.ServiceInfo
	Hash         string
}
