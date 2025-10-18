package gxyactor

import (
	"context"
	"fmt"
	"gserver/core/gxylocator"
	"gserver/core/gxylog"
	"gserver/core/gxyregistery"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"
	"hash/fnv"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/asynkron/protoactor-go/router"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"google.golang.org/protobuf/encoding/protojson"
)

type grainInfo struct {
	Kind      string
	Props     *actor.Props
	Activator PID
}

type grainActivator struct {
	kind    string
	manager *grainManager
	timer   *ActorTimer
	ctx     context.Context
	childs  map[PID]string
}

type ActorCheckResult struct {
	ID  string
	Pid PID
	Err error
}

const (
	CONTEXT_KEY_ID = "id"
)

type GrainContext struct {
	actor.Context
	Metadata map[string]any
}

func (ctx *GrainContext) MetadataValue(key string) any {
	if val, ok := ctx.Metadata[key]; ok {
		return val
	}
	return ""
}

func NewGrainActivator(kind string, manager *grainManager) *grainActivator {
	return &grainActivator{
		kind:    kind,
		manager: manager,
		ctx:     gxylog.NewContext(context.Background(), "grain_activator"),
	}
}

func (a *grainActivator) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		a.timer = NewActorTimer(ctx.Self(), nil)
		a.timer.AddTick(a.ctx, &gxytimer.Tick{
			Name:     "update_register",
			Interval: 10 * time.Second,
		}, func(c context.Context, info gxytimer.TimerActiveInfo) {
			a.UpdateRegister(c, info, ctx.Children())
		})
	case ActorTimerMsg:
		a.timer.Active(a.ctx, msg)
	case *pb.ActorActive:
		supervisor := newSupervisor()
		ginfo, ok := a.manager.grainInfos[a.kind]
		system := ctx.ActorSystem()
		if !ok {
			ctx.Respond(&pb.ActorError{
				Reason: fmt.Sprintf("grain kind %s not registered", a.kind),
			})
			return
		}

		pid, err := ctx.SpawnNamed(ginfo.Props.Configure(actor.WithSupervisor(supervisor),
			actor.WithContextDecorator(func(next actor.ContextDecoratorFunc) actor.ContextDecoratorFunc {
				return func(ctx actor.Context) actor.Context {
					return &GrainContext{
						Context: ctx,
						Metadata: map[string]any{
							CONTEXT_KEY_ID: msg.Id,
						},
					}
				}
			}),
		), msg.Id)
		if err != nil {
			if err == actor.ErrNameExists {
				// actor already exists, return its pid
				ctx.Respond(&remote.ActorPidResponse{
					Pid: pid,
				})
				return
			}
			glog.Errorf(a.ctx, "spawn grain actor failed: %s", err)
			ctx.Respond(&pb.ActorError{
				Reason: err.Error(),
			})
			return
		}
		sender := ctx.Sender()
		self := ctx.Self()
		go func() {
			// touch actor to check if it is started
			err = system.Root.RequestFuture(pid, &actor.Touch{}, 2*time.Second).Wait()
			system.Root.RequestWithCustomSender(self, &ActorCheckResult{
				ID:  msg.Id,
				Pid: pid,
				Err: err,
			}, sender)
		}()

	case *ActorCheckResult:
		if msg.Err != nil {
			glog.Errorf(a.ctx, "touch grain actor failed: %v", msg.Err)
			ctx.Respond(&pb.ActorError{
				Reason: "start grain failed",
			})
			return
		}
		key := getGrainLocateKey(a.kind, msg.ID)
		pid := msg.Pid
		err := a.registerGrainNode(a.ctx, key, pid)
		if err != nil {
			glog.Errorf(a.ctx, "register grain node failed: %v", err)
			ctx.Stop(pid)
			ctx.Respond(&pb.ActorError{
				Reason: err.Error(),
			})
			return
		}
		a.childs[pid] = msg.ID
		ctx.Respond(&remote.ActorPidResponse{
			Pid: pid,
		})
	case *actor.Terminated:
		child := msg.Who
		if child == nil {
			return
		}
		id := a.childs[child]
		if id == "" {
			return
		}
		key := getGrainLocateKey(a.kind, id)
		err := a.manager.grainLocator.UnregisterNode(a.ctx, key)
		if err != nil {
			glog.Errorf(a.ctx, "unregister grain node failed: %v", err)
		}
		delete(a.childs, child)
	case *actor.Stop:
		glog.Info(a.ctx, "grain activator actor stopped")
	}
}

func (a *grainActivator) registerGrainNode(ctx context.Context, key string, pid PID) error {
	pidInfo, _ := protojson.Marshal(&pb.ActorPid{
		Address: pid.Address,
		Id:      pid.Id,
	})
	return a.manager.grainLocator.RegisterNode(ctx, key, string(pidInfo), 15*time.Second)
}

func (a *grainActivator) UpdateRegister(ctx context.Context, _info gxytimer.TimerActiveInfo, children []PID) {
	for pid, childID := range a.childs {
		key := getGrainLocateKey(a.kind, childID)
		err := a.registerGrainNode(ctx, key, pid)
		if err != nil {
			glog.Errorf(a.ctx, "register grain node failed: %v", err)
		}
	}
}

type grainManager struct {
	sys          *actorSystem
	grainLocator *gxylocator.Locator
	grainInfos   map[string]*grainInfo
}

func NewGrainManager(sys *actorSystem) *grainManager {
	return &grainManager{
		sys:        sys,
		grainInfos: make(map[string]*grainInfo),
	}
}

func (g *grainManager) Init(ctx context.Context) error {
	g.grainLocator = gxylocator.NewLocator("gserver")
	return nil
}

func (g *grainManager) Stop(ctx context.Context) error {
	for _, ginfo := range g.grainInfos {
		g.sys.system.Root.Stop(ginfo.Activator)
	}
	return nil
}

func (g *grainManager) grainReciveMiddleware(_ string) actor.ReceiverMiddleware {
	return func(next actor.ReceiverFunc) actor.ReceiverFunc {
		return func(ctx actor.ReceiverContext, envelope *actor.MessageEnvelope) {
			// key := getGrainLocateKey(kind, ctx.Value(actor.KeyId).(string))
			// err := g.grainLocator.RegisterNode(context.Background(), key, g.sys.Host())
			// if err != nil {
			// 	return nil, err
			// }
			next(ctx, envelope)
		}
	}
}

func (g *grainManager) getManagerName(kind string) string {
	return fmt.Sprintf("%s_%s", "GrainManager", kind)
}

func (g *grainManager) RegisterGrain(kind string, props actor.Producer) error {
	newProps := actor.PropsFromProducer(props,
		actor.WithReceiverMiddleware(g.grainReciveMiddleware(kind)))
	ginfo := &grainInfo{
		Kind:  kind,
		Props: newProps,
	}
	pid, err := g.sys.system.Root.SpawnNamed(
		router.NewConsistentHashPool(5, actor.WithProducer(func() actor.Actor {
			return NewGrainActivator(kind, g)
		})), g.getManagerName(kind))
	if err != nil {
		return err
	}
	ginfo.Activator = pid
	g.grainInfos[kind] = ginfo
	return nil
}

func (g *grainManager) spawnGrain(node string, kind string, id string) (PID, error) {
	glog.Debugf(context.Background(), "spawn grain %s:%s at %s", kind, id, node)
	activator := actor.NewPID(node, g.getManagerName(kind))
	rsp, err := g.sys.Call(activator, &pb.ActorActive{
		Kind: kind,
		Id:   id,
	})
	if err != nil {
		return nil, err
	}
	if rsp, ok := rsp.(*pb.ActorError); ok {
		return nil, gerror.New(rsp.Reason)
	}
	return rsp.(*remote.ActorPidResponse).Pid, nil
}

func (g *grainManager) getGrain(kind string, id string, spawn bool) (PID, error) {
	ctx := context.Background()
	key := getGrainLocateKey(kind, id)
	pidInfo, err := g.grainLocator.LocateNode(ctx, key)
	if err != nil {
		return nil, err
	}
	if pidInfo != "" {
		var pid pb.ActorPid
		err := protojson.Unmarshal([]byte(pidInfo), &pid)
		if err != nil {
			return nil, err
		}
		return actor.NewPID(pid.Address, pid.Id), nil
	}
	if !spawn {
		return nil, gerror.Newf("grain %s:%s not found", kind, id)
	}

	grainNode := ServiceManager().GetServiceNode(kind, key, gxyregistery.ConsistentHashSelector())

	if grainNode.Node == "" {
		return nil, gerror.Newf("find grain node failed, kind: %s, id: %s", kind, id)
	}
	return g.spawnGrain(grainNode.Node, kind, id)
}

// 建议：考虑数据分布，避免热点
func getGrainLocateKey(kind string, id string) string {
	// 添加哈希前缀，改善Redis分布
	fnv32a := fnv.New32a()
	hash := fnv32a.Sum([]byte(kind + id))
	return fmt.Sprintf("actor:%02d:%s:%s", hash, kind, id)
}
