// service.go 定义"可注册服务"的抽象:业务 App 内嵌 Service 即成为可注册服务,
// 注册时以 ServiceName/Host/Version/Weight 描述自己。
package gxyservice

import (
	"gserver/core/gxymodule"
)

const (
	DEFAULT_WEIGHT  = 1
	DEFAULT_VERSION = "v1.0.0"
)

// IService 是可注册服务:Actor 模块 + 注册所需的元信息。
// Host 由业务 App 提供(端口在节点内不唯一),其余三项有默认实现。
type IService interface {
	gxymodule.IModule
	ServiceName() string
	Weight() int
	Version() string
	Host() string
}

// Service 供业务 App 内嵌,提供默认的注册元信息。
type Service struct {
	gxymodule.ModuleBase
}

// ServiceName 以模块名作为服务名,与模块树中的名字保持一致。
func (s *Service) ServiceName() string {
	return s.GetModName()
}

// Weight 返回默认权重。
func (s *Service) Weight() int {
	return DEFAULT_WEIGHT
}

// Version 返回默认版本号。
func (s *Service) Version() string {
	return DEFAULT_VERSION
}
