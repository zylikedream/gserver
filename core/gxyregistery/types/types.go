// types 包定义服务发现在注册中心与各后端之间传递的描述符。
// 它只依赖 fmt/gjson/gsvc,不引入 gserver 包:后端实现(consul/redis)与
// 门面(gxyregistery)都引用它,避免出现影子结构体。
package types

import (
	"fmt"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/net/gsvc"
)

const (
	SERVICE_NAMESPACE = "gserver"
)

type ServiceState string

const (
	ServiceStateServing     ServiceState = "serving"
	ServiceStateDraining    ServiceState = "draining"
	ServiceStateMaintaining ServiceState = "maintaining"
)

type ServiceData struct {
	Name     string
	NodeName string
	Version  string
	Weight   int
	NodeHost string
	State    ServiceState
}

type ServiceInfo struct {
	ServiceData
}

func NewServiceInfo(name string, nodeName string, nodeHost string, version string, weight int) *ServiceInfo {
	return &ServiceInfo{
		ServiceData: ServiceData{
			Name:     name,
			NodeName: nodeName,
			Version:  version,
			Weight:   weight,
			NodeHost: nodeHost,
			State:    ServiceStateServing,
		},
	}
}

func NewServiceFromBytes(data []byte) (*ServiceInfo, error) {
	sv := ServiceData{}
	if err := gjson.Unmarshal(data, &sv); err != nil {
		return nil, err
	}
	serviceInfo := &ServiceInfo{
		ServiceData: sv,
	}
	return serviceInfo.withDefaultState(), nil
}

func (s *ServiceInfo) withDefaultState() *ServiceInfo {
	if s.State == "" {
		s.State = ServiceStateServing
	}
	return s
}

func (s *ServiceInfo) GetName() string {
	return s.Name
}

func (s *ServiceInfo) GetVersion() string {
	return s.Version
}

// ServiceKey 返回服务实例在注册表中的唯一键,键值型后端(如 redis)用它做字段名。
func ServiceKey(nodeName, name, nodeHost string) string {
	return fmt.Sprintf("%s-%s-%s:%s", SERVICE_NAMESPACE, nodeName, name, nodeHost)
}

func (s *ServiceInfo) GetKey() string {
	return ServiceKey(s.NodeName, s.Name, s.NodeHost)
}

func (s *ServiceInfo) GetPrefix() string {
	return fmt.Sprintf("%s-%s-%s", SERVICE_NAMESPACE, s.NodeName, s.Name)
}

func (s *ServiceInfo) GetValue() string {
	value, _ := gjson.Marshal(s.ServiceData)
	return string(value)
}

func (s *ServiceInfo) GetEndpoints() gsvc.Endpoints {
	return gsvc.NewEndpoints(s.NodeHost)
}

func (s *ServiceInfo) GetMetadata() gsvc.Metadata {
	return map[string]any{
		"weight":    s.Weight,
		"node_name": s.NodeName,
		"host":      s.NodeHost,
		"state":     s.State,
	}
}

func (s *ServiceInfo) IsServing() bool {
	return s != nil && (s.State == "" || s.State == ServiceStateServing)
}
