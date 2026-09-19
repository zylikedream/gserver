package gxyactor

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"

	"ergo.services/ergo"
	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
)

// actorApp 基础Actor模块:持有 ergo 节点,是整个 actor 运行时的入口。
type actorApp struct {
	gxyapp.App
	node      gen.Node
	nodeName  string
	host      string
	activator *activatorManager
}

var app *actorApp

// ActorApp 获取基础Actor模块。
func ActorApp() *actorApp {
	return app
}

func (a *actorApp) NodeName() string {
	return a.nodeName
}

func (a *actorApp) Host() string {
	return a.host
}

// NewActorApp 创建基础Actor模块。
// nodeInstanceName 已废弃:路由身份用稳定节点名,所有权身份由运行时节点启动时刻承担(ADR 0010)。
func NewActorApp(nodeName string, nodeInstanceName string, host string) *actorApp {
	app = &actorApp{
		nodeName: nodeName,
		host:     host,
	}
	return app
}

// OnModInit 启动 ergo 节点。
func (a *actorApp) OnModInit(ctx context.Context) error {
	port := g.Cfg().MustGet(ctx, "port.actor").Int()

	options := gen.NodeOptions{}
	options.Network.Mode = gen.NetworkModeEnabled
	// 节点互连的共享密钥。为空时运行时会为每个节点生成随机值,
	// 导致跨节点握手失败(单节点自测不受影响),因此必须显式配置。
	cookie := g.Cfg().MustGet(ctx, "network.cookie").String()
	if cookie == "" {
		gxylog.Warn(ctx, "network cookie is empty; cross-node communication will fail",
			gxylog.Str("node", a.nodeName))
	}
	options.Network.Cookie = cookie
	// 节点发现接项目已有的服务注册表:运行时只按节点名解析地址,
	// 适配器据此从注册表取回地址(见 ADR 0011)。
	options.Network.Registrar = newServiceRegistrar()
	options.Network.Acceptors = []gen.AcceptorOptions{
		{
			Host: a.host,
			Port: uint16(port),
		},
	}
	// 必须从默认值派生:写字面量会让未列出项保持 false,静默关闭能力(见 ergo-foundation §5)。
	flags := gen.DefaultNetworkFlags
	options.Network.Flags = flags

	options.Log.Level = gen.LogLevelInfo
	// 必须显式关闭默认 logger,否则与 gxylog 的控制台输出重复(见 ADR 0013)。
	options.Log.DefaultLogger.Disable = true
	options.Log.Loggers = []gen.Logger{
		{Name: "gxylog", Logger: newErgoLogger()},
	}

	nodeName := gen.Atom(a.nodeName + "@" + a.host)
	node, err := ergo.StartNode(nodeName, options)
	if err != nil {
		return gerror.Wrapf(err, "start ergo node %s", nodeName)
	}
	a.node = node

	// 跨节点消息统一走信封,因此只需向运行时注册这一个类型(见 ADR 0009)。
	if err := node.Network().RegisterType(WireEnvelope{}); err != nil {
		node.Stop()
		return gerror.Wrap(err, "register wire envelope")
	}

	a.activator = NewActivatorManager(a.nodeName, string(nodeName))
	if err := a.activator.OnModInit(ctx); err != nil {
		node.Stop()
		return err
	}
	return nil
}

func (a *actorApp) OnModStart(ctx context.Context) error {
	if err := a.activator.OnModStart(ctx); err != nil {
		return err
	}
	// 登记节点级地址记录,使其它节点能按节点名解析到本节点的 actor 地址。
	// 与承载哪些能力无关,因此纯网关节点也能被解析(见 ADR 0011)。
	if svc := gxyservice.ServiceApp(); svc != nil {
		svc.LoadService(ctx, &ActorNodeService{})
	}

	gxylog.Info(ctx, "actor started", gxylog.Str("nodeName", a.nodeName), gxylog.Str("address", a.Address()))
	return nil
}

// OnModStop 优雅停止节点。
//
// 必须用优雅停止:它会等待所有进程的终止回调返回,而终止回调里包含最终落库
// 与所有权释放。强制停止会跳过该等待,导致每次停机静默丢失最后一次存盘
// (见 invariants.md #10)。
func (a *actorApp) OnModStop(ctx context.Context) error {
	if a.activator != nil {
		if err := a.activator.OnModStop(ctx); err != nil {
			gxylog.Error(ctx, "stop activator failed", gxylog.Err(err))
		}
	}
	if a.node != nil {
		a.node.Stop()
	}
	gxylog.Info(ctx, "actor system stopped", gxylog.Str("address", a.Address()))
	return nil
}

func (a *actorApp) RegisterActorKind(name string, prod ActorProducer) error {
	return a.activator.RegisterActorKind(name, prod)
}

func (a *actorApp) DeregisterActorKind(name string) {
	a.activator.DeregisterActorKind(name)
}

// spawnNamed 以名字注册方式创建 actor。
// 名字在初始化之前由运行时原子注册,因此并发同名创建是良性的:
// 败者既不执行初始化,也不执行终止。
func (a *actorApp) spawnNamed(kind string, name string, prod ActorProducer, initArgs ...any) (PID, error) {
	if a.node == nil {
		return PID{}, gerror.New("actor node not initialized")
	}
	pid, err := a.node.SpawnRegister(gen.Atom(name), asFactory(kind, prod), gen.ProcessOptions{}, initArgs...)
	if err != nil {
		return PID{}, err
	}
	return pidFromLocal(pid), nil
}

// spawnUnnamed 创建一个不带名字的 actor。
// 这类实例不能按名寻址,生命周期由创建者负责(会话、临时 worker)。
func (a *actorApp) spawnUnnamed(prod ActorProducer, initArgs ...any) (PID, error) {
	if a.node == nil {
		return PID{}, gerror.New("actor node not initialized")
	}
	// 无名实例不参与按名寻址,没有注册表的键可作为权威能力名,沿用构造时的标签。
	pid, err := a.node.Spawn(asFactory("", prod), gen.ProcessOptions{}, initArgs...)
	if err != nil {
		return PID{}, err
	}
	return pidFromLocal(pid), nil
}

// send 向进程发送消息。跨节点时自动装信封(见 ADR 0009)。
func (a *actorApp) send(ctx context.Context, pid PID, message any) error {
	if a.node == nil {
		return gerror.New("actor node not initialized")
	}
	target := pid.target()
	if target == nil {
		return gerror.New("send to empty pid")
	}
	out, err := a.prepareOutbound(message, pid.Node())
	if err != nil {
		return err
	}
	return a.node.Send(target, out)
}

// callImportant 同步调用,投递失败会立即返回错误而非超时。
// 目标可以是 PID 或 ProcessID(远端按名寻址);跨节点时自动装信封。
func (a *actorApp) callImportant(ctx context.Context, target any, message any) (any, error) {
	if a.node == nil {
		return nil, gerror.New("actor node not initialized")
	}
	out, err := a.prepareOutbound(message, string(targetNode(target)))
	if err != nil {
		return nil, err
	}
	result, err := a.node.CallImportant(target, out)
	if err != nil {
		return nil, err
	}
	result, err = UnwrapWire(result)
	if err != nil {
		return nil, err
	}
	if aerr, ok := result.(*pb.ActorError); ok {
		return nil, gerror.New(aerr.Reason)
	}
	return result, nil
}

// prepareOutbound 决定是否需要装信封:只有发往其他节点的消息才需要,
// 本机投递保持零拷贝。
func (a *actorApp) prepareOutbound(message any, node string) (any, error) {
	if a.node == nil || node == "" || node == string(a.node.Name()) {
		return message, nil
	}
	return packForWire(message)
}

// targetNode 取出寻址目标所属的节点名。
func targetNode(target any) gen.Atom {
	switch t := target.(type) {
	case gen.PID:
		return t.Node
	case *gen.PID:
		return t.Node
	case gen.ProcessID:
		return t.Node
	case *gen.ProcessID:
		return t.Node
	default:
		return ""
	}
}

func (a *actorApp) GetNodeName() string {
	return a.nodeName
}

func (a *actorApp) StopActor(pid PID) error {
	if a.node == nil {
		return gerror.New("actor node not initialized")
	}
	target := pid.target()
	if target == nil {
		return gerror.New("stop actor: empty pid")
	}
	return a.node.SendExit(target.(gen.PID), gen.TerminateReasonNormal)
}

func (a *actorApp) NodeInstanceName() string {
	return string(a.node.Name())
}

// Address 返回本节点的 actor 协议监听地址(host:port)。
//
// 服务注册需要该地址:其它节点据此建立连接。不能用运行时节点名——它的格式
// 是 name@host,不含端口(见 ADR 0011)。
func (a *actorApp) Address() string {
	if a.node == nil {
		return ""
	}
	acceptors, err := a.node.Network().Acceptors()
	if err != nil || len(acceptors) == 0 {
		return ""
	}
	return acceptors[0].Info().Interface
}

// address 包级入口,供 ActorService 使用。
func address() string {
	if app == nil {
		return ""
	}
	return app.Address()
}

func (a *actorApp) GetActorOwner(ctx context.Context, kind string, id string) (ActorOwner, error) {
	if a.activator == nil || a.activator.locator == nil {
		return ActorOwner{}, gerror.New("actor locator is not initialized")
	}
	return a.activator.locator.locate(ctx, kind, id)
}

func (a *actorApp) ActivateActor(ctx context.Context, kind string, id string, spawn bool) (PID, error) {
	return a.activator.getActor(ctx, kind, id, spawn)
}

func (a *actorApp) GetActorCount(kind string) int {
	return a.activator.GetActorCount(kind)
}

func (a *actorApp) GetLocalActor(kind string, id string) PID {
	return a.activator.GetLocalActor(kind, id)
}

func (a *actorApp) GetLocalActorAll(kind string) []PID {
	return a.activator.GetLocalActorAll(kind)
}
