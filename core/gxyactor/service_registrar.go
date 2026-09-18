package gxyactor

import (
	"context"
	"net"
	"strconv"
	"strings"

	"gserver/core/gxylog"
	"gserver/core/gxyservice"

	"ergo.services/ergo/gen"
)

// serviceRegistrar 把运行时（ergo）的节点发现接到项目已有的服务注册表上。
//
// 运行时只按**节点名**解析连接地址（`name@host`），而项目的注册表按
// **能力名**组织记录；两者的桥是命名约定：节点名 "@" 之前的部分即能力名
// （生产配置一节点一能力，故该约定成立）。
//
// 本适配器不额外写任何注册记录 —— 节点地址已由服务组件写入注册中心
// （见 ADR 0011）。
type serviceRegistrar struct {
	// handshake/proto 版本取自本节点自身的路由信息。
	// 解析接口返回的路由没有"缺省补全"这一步，必须显式带上。
	handshake gen.Version
	proto     gen.Version

	selfNode string
}

func newServiceRegistrar() *serviceRegistrar {
	return &serviceRegistrar{
		handshake: gen.Version{Name: "gserver-registrar", Release: "1"},
		proto:     gen.Version{Name: "gserver-registrar", Release: "1"},
	}
}

// Register 由运行时在节点网络启动时调用。
// 不写注册记录:节点地址已由服务组件登记;仅记录本节点的协议版本供解析使用。
func (r *serviceRegistrar) Register(node gen.NodeRegistrar, routes gen.RegisterRoutes) (gen.StaticRoutes, error) {
	if node != nil {
		r.selfNode = string(node.Name())
	}
	if len(routes.Routes) > 0 {
		r.handshake = routes.Routes[0].HandshakeVersion
		r.proto = routes.Routes[0].ProtoVersion
	}
	return gen.StaticRoutes{}, nil
}

func (r *serviceRegistrar) Resolver() gen.Resolver { return r }

// Resolve 把节点名解析为可达地址。
//
// 地址取自节点级服务记录(见 ActorNodeService):运行时的入口只给节点名,
// 而一个节点可以承载多种能力、也可以一种都不承载,因此按能力名反推地址
// 不成立。
func (r *serviceRegistrar) Resolve(node gen.Atom) ([]gen.Route, error) {
	name := string(node)
	_, host, ok := splitNodeName(name)
	if !ok {
		return nil, gen.ErrIncorrect
	}

	addr := resolveNodeHost(ActorNodeServiceName, name)
	if addr == "" {
		return nil, gen.ErrUnknown
	}

	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		gxylog.Warn(context.Background(), "invalid node address in registry",
			gxylog.Str("node", name), gxylog.Str("address", addr), gxylog.Err(err))
		return nil, gen.ErrUnknown
	}
	port, err := strconv.ParseUint(p, 10, 16)
	if err != nil {
		return nil, gen.ErrUnknown
	}
	if h == "" {
		// 记录里是 "0.0.0.0:port" 这类通配地址时,用节点名里的 host 兜底。
		h = host
	}

	return []gen.Route{{
		Host:             h,
		Port:             uint16(port),
		HandshakeVersion: r.handshake,
		ProtoVersion:     r.proto,
	}}, nil
}

func (r *serviceRegistrar) ResolveProxy(gen.Atom) ([]gen.ProxyRoute, error) {
	return nil, gen.ErrUnsupported
}

func (r *serviceRegistrar) ResolveApplication(gen.Atom) (gen.ApplicationRoutes, error) {
	return nil, gen.ErrUnsupported
}

// 以下能力本项目不使用,按接口要求如实声明为不支持。

func (r *serviceRegistrar) RegisterProxy(gen.Atom) error   { return gen.ErrUnsupported }
func (r *serviceRegistrar) UnregisterProxy(gen.Atom) error { return gen.ErrUnsupported }

func (r *serviceRegistrar) RegisterApplicationRoute(gen.ApplicationRoute) error {
	return gen.ErrUnsupported
}

func (r *serviceRegistrar) UnregisterApplicationRoute(gen.Atom) error {
	return gen.ErrUnsupported
}

func (r *serviceRegistrar) Nodes() ([]gen.Atom, error) { return nil, gen.ErrUnsupported }
func (r *serviceRegistrar) Config(...string) (map[string]any, error) {
	return nil, gen.ErrUnsupported
}
func (r *serviceRegistrar) ConfigItem(string) (any, error) { return nil, gen.ErrUnsupported }
func (r *serviceRegistrar) Event() (gen.Event, error)      { return gen.Event{}, gen.ErrUnsupported }
func (r *serviceRegistrar) Terminate()                     {}

func (r *serviceRegistrar) Version() gen.Version {
	return gen.Version{Name: "gserver-service-registry", Release: "1"}
}

func (r *serviceRegistrar) Info() gen.RegistrarInfo {
	return gen.RegistrarInfo{
		Server:                     "gxyservice",
		EmbeddedServer:             true,
		SupportRegisterProxy:       false,
		SupportRegisterApplication: false,
		SupportConfig:              false,
		SupportEvent:               false,
		Version:                    r.Version(),
	}
}

// splitNodeName 拆分节点名 "能力@主机"。
func splitNodeName(name string) (kind string, host string, ok bool) {
	at := strings.Index(name, "@")
	if at <= 0 || at == len(name)-1 {
		return "", "", false
	}
	return name[:at], name[at+1:], true
}

// resolveNodeHost 从服务注册表查询指定节点在某服务下的地址。
// 按节点名(而非能力名)过滤,因此一个记录下可以并存多个节点。
func resolveNodeHost(serviceName string, nodeName string) string {
	svc := gxyservice.ServiceApp()
	if svc == nil {
		return ""
	}
	return svc.GetAddressByNodeName(context.Background(), serviceName, nodeName)
}
