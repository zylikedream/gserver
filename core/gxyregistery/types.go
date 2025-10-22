package gxyregistery

import (
	"fmt"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/net/gsvc"
)

const (
	SERVICE_NAMESPACE = "gserver"
)

type HashServices struct {
	ServiceInfos []*ServiceInfo
	Hash         string
}

type ServiceData struct {
	Name     string
	NodeName string
	Version  string
	Weight   int
	NodeHost string
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
		},
	}
}

func NewServiceFromBytes(data []byte) (*ServiceInfo, error) {
	sv := ServiceData{}
	if err := gjson.Unmarshal(data, &sv); err != nil {
		return nil, err
	}
	return &ServiceInfo{
		ServiceData: sv,
	}, nil
}

func (s *ServiceInfo) GetName() string {
	return s.Name
}

func (s *ServiceInfo) GetVersion() string {
	return s.Version
}

func (s *ServiceInfo) GetKey() string {
	return fmt.Sprintf("%s:%s", s.GetPrefix(), s.NodeHost)
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
	}
}
