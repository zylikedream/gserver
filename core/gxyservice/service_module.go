package gxyservice

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxymodule"

	"ergo.services/ergo/gen"
)

type ServiceNode struct {
	Name string
	Node string
}

type serviceModule struct {
	gxymodule.Module
	Services []IService
}

var svrMod *serviceModule

func ServiceModule() *serviceModule {
	return svrMod
}

func NewService() *serviceModule {
	svrMod = &serviceModule{}
	return svrMod
}

func (s *serviceModule) GetServices() []IService {
	return s.Services
}

func (s *serviceModule) LoadService(service IService) {
	s.Services = append(s.Services, service)
}

func (s *serviceModule) OnStart(ctx context.Context) error {
	node := gxyactor.ActorSystem().GetNode()
	reg, _ := node.Network().Registrar()
	for _, s := range s.Services {
		if err := s.OnStart(ctx); err != nil {
			return err
		}
		worker := s.Worker()
		if worker == nil {
			continue
		}
		node.Network().EnableSpawn(gen.Atom(s.Name()), gen.ProcessFactory(s.Worker()))
		reg.RegisterApplicationRoute(gen.ApplicationRoute{
			Name: gen.Atom(string(s.Name())),
			Node: gen.Atom(node.Name()),
		})
	}
	return nil
}

func (s *serviceModule) GetServiceNode(service string, selector ServiceSelector) ServiceNode {
	node := gxyactor.ActorSystem().GetNode()
	reg, _ := node.Network().Registrar()
	resolver := reg.Resolver()
	routes, err := resolver.ResolveApplication(gen.Atom(string(service)))
	if err != nil {
		return ServiceNode{}
	}
	serviceNodes := make([]ServiceNode, 0)
	for _, route := range routes {
		serviceNodes = append(serviceNodes, ServiceNode{
			Name: service,
			Node: route.Node.String(),
		})
	}
	return selector.Select(service, serviceNodes)
}
