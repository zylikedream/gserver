package gxyactor

import (
	"context"
	"fmt"
	"gserver/core/gxylocator"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/asynkron/protoactor-go/remote"
	"github.com/asynkron/protoactor-go/router"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	GrainLocateTTL            = 40 * time.Second
	GrainLocateUpdateInterval = 30 * time.Second
)

type grainMeta struct {
	Kind      string
	Props     *actor.Props
	Activator PID
	mgr       *ActorMgr
}

type grainActivator struct {
	*ActorBase
	kind    string
	manager *grainManager
	childs  map[PID]string
	meta    *grainMeta
}

type ActorCheckResult struct {
	ID     string
	Pid    PID
	Err    error
	Sender PID
}

func NewGrainActivator(kind string, manager *grainManager) *grainActivator {
	a := &grainActivator{
		kind:    kind,
		manager: manager,
		childs:  make(map[PID]string),
	}
	ctx := gxylog.NewContext(context.Background(), "grain_activator")
	a.ActorBase = NewActorBase(ctx, a)
	return a
}

func (a *grainActivator) DelayInit(ctx context.Context) error {
	ginfo, ok := a.manager.grainMetas[a.kind]
	if !ok {
		return fmt.Errorf("grain kind %s not registered", a.kind)
	}
	a.meta = ginfo
	return nil
}

func (a *grainActivator) HandleMessage(ctx context.Context, msg any) error {
	switch msg := msg.(type) {
	case *pb.ActorActive:
		props := a.meta.Props.Clone()
		pid, err := a.SpawnNamed(props.Configure(
			actor.WithContextDecorator(ContextDecorator(msg.Id, a.kind)),
			actor.WithReceiverMiddleware(a.grainReciveMiddleware())),
			msg.Id)
		if err != nil {
			if err == actor.ErrNameExists {
				// actor already exists, return its pid
				a.Respond(&remote.ActorPidResponse{
					Pid: pid,
				})
				return nil
			}
			glog.Errorf(ctx, "spawn grain actor failed: %s", err)
			a.Respond(ActorError(err.Error()))
			return nil
		}
		sender := a.Actx.Sender()
		go func() {
			// touch actor to check if it is started
			// Notice: don't use actor ctx in goroutine, especially respond method, because sender will be cleared after recieve!!
			_, err = a.Call(pid, &actor.Touch{}, 2*time.Second)
			Send(a.Self(), &ActorCheckResult{
				ID:     msg.Id,
				Pid:    pid,
				Err:    err,
				Sender: sender,
			})
		}()

	case *ActorCheckResult:
		sender := msg.Sender
		if msg.Err != nil {
			glog.Errorf(ctx, "touch grain actor failed: %v", msg.Err)
			a.Send(sender, &pb.ActorError{
				Reason: "start grain failed",
			})
			return nil
		}
		pid := msg.Pid
		a.childs[pid] = msg.ID
		a.meta.mgr.Add(msg.ID, pid)
		a.Send(sender, &remote.ActorPidResponse{
			Pid: pid,
		})
	case *actor.Terminated: // child actor terminated
		child := msg.Who
		if child == nil {
			return nil
		}
		id := a.childs[child]
		if id == "" {
			return nil
		}
		a.meta.mgr.Remove(id)
		delete(a.childs, child)
	}
	return nil
}

func (a *grainActivator) Terminate(ctx context.Context, err error) {
	glog.Info(ctx, "grain activator actor stopped")
}

func (a *grainActivator) registerGrainNode(ctx context.Context, key string, pid PID) error {
	pidInfo, _ := protojson.Marshal(&pb.ActorPid{
		Address: pid.Address,
		Id:      pid.Id,
	})
	return a.manager.grainLocator.RegisterNode(ctx, key, string(pidInfo), GrainLocateTTL)
}

func (a *grainActivator) DeregisterGrainNode(ctx context.Context, key string, pid PID) error {
	pidInfo, _ := protojson.Marshal(&pb.ActorPid{
		Address: pid.Address,
		Id:      pid.Id,
	})
	return a.manager.grainLocator.UnregisterNode(ctx, key, string(pidInfo))
}

func (g *grainActivator) grainReciveMiddleware() actor.ReceiverMiddleware {
	return func(next actor.ReceiverFunc) actor.ReceiverFunc {
		return func(ctx actor.ReceiverContext, envelope *actor.MessageEnvelope) {

			switch envelope.Message.(type) {
			case *ActorInitMsg:
				err := g.registerGrain(ctx)
				if err != nil {
					glog.Errorf(g.ctx, "register grain node failed: %v", err)
					envelope.Message = &ActorInternalError{
						err: err,
					}
					next(ctx, envelope)
					return
				}
			case *actor.Stopped:
				err := g.deregisterGrain(ctx)
				if err != nil {
					glog.Errorf(g.ctx, "unregister grain node failed: %v", err)
				}
			}
			next(ctx, envelope)
		}
	}
}

func (g *grainActivator) registerGrain(ctx actor.ReceiverContext) error {
	act, ok := ctx.Actor().(IActor)
	if !ok {
		return gerror.New("actor type error")
	}
	self := ctx.Self()
	actorCtx, ok := ctx.(*ActorContext)
	if !ok {
		return gerror.New("grain context type error")
	}
	id := actorCtx.InitArgs[0].(string)
	kind := actorCtx.InitArgs[1].(string)
	key := getGrainLocateKey(kind, id)
	err := g.registerGrainNode(g.ctx, key, self)
	if err != nil {
		return err
	}
	act.Timer().AddTick(g.ctx, &gxytimer.Tick{
		Name:     "locate_tick",
		Interval: GrainLocateUpdateInterval,
	}, func(ctx context.Context, _ gxytimer.TimerActiveInfo) {
		err := g.registerGrainNode(g.ctx, key, self)
		if err != nil {
			glog.Errorf(g.ctx, "register grain %s node failed: %v", key, err)
		}
	})
	glog.Debugf(g.ctx, "register grain node %s:%s", key, self.String())
	return nil
}

func (g *grainActivator) deregisterGrain(ctx actor.ReceiverContext) error {
	self := ctx.Self()
	actorCtx, ok := ctx.(*ActorContext)
	if !ok {
		return gerror.New("grain context type error")
	}

	id := actorCtx.InitArgs[0].(string)
	kind := actorCtx.InitArgs[1].(string)
	key := getGrainLocateKey(kind, id)
	err := g.DeregisterGrainNode(g.ctx, key, self)
	if err != nil {
		return gerror.Newf("unregister grain node failed: %v", err)
	}
	return nil
}

type grainManager struct {
	gxymodule.ModuleBase
	grainLocator *gxylocator.Locator
	grainMetas   map[string]*grainMeta
	ctx          context.Context
}

func NewGrainManager() *grainManager {
	return &grainManager{
		grainMetas: make(map[string]*grainMeta),
		ctx:        gxylog.NewContext(context.Background(), "grainManager"),
	}
}

func (g *grainManager) OnModInit(ctx context.Context) error {
	g.grainLocator = gxylocator.NewLocator("gserver")
	return nil
}

func (g *grainManager) OnModStop(ctx context.Context) error {
	for _, ginfo := range g.grainMetas {
		StopActor(ginfo.Activator)
	}
	return nil
}

func (g *grainManager) getManagerName(kind string) string {
	return fmt.Sprintf("%s_%s", "GrainManager", kind)
}

func (g *grainManager) RegisterGrain(kind string, props GrainProducer) error {
	grainProps := actor.PropsFromProducer(func() actor.Actor {
		return props()
	}, actor.WithSupervisor(newSupervisor()))
	gmeta := &grainMeta{
		Kind:  kind,
		Props: grainProps,
	}
	g.grainMetas[kind] = gmeta
	pid, err := SpawnNamed(g.getManagerName(kind),
		router.NewConsistentHashPool(5, actor.WithProducer(func() actor.Actor {
			return NewGrainActivator(kind, g)
		})))
	if err != nil {
		g.grainMetas[kind] = nil
		return err
	}
	gmeta.Activator = pid
	gmeta.mgr = NewActorMgr(fmt.Sprintf("%s_%s", "grainMgr", kind))
	return nil
}

func (g *grainManager) DeRegisterGrain(kind string) {
	ginfo, ok := g.grainMetas[kind]
	if !ok {
		return
	}
	StopActor(ginfo.Activator)
	delete(g.grainMetas, kind)
}

func (g *grainManager) spawnGrain(node string, kind string, id string) (PID, error) {
	glog.Debugf(g.ctx, "spawn grain %s:%s at %s", kind, id, node)
	activator := actor.NewPID(node, g.getManagerName(kind))
	rsp, err := Call(activator, &pb.ActorActive{
		Kind: kind,
		Id:   id,
	}, -1)
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
		return nil, nil
	}

	grainInfo := gxyservice.ServiceApp().GetServiceInfo(ctx, kind, key, gxyregistery.ConsistentHashSelector())

	if grainInfo == nil || grainInfo.NodeHost == "" {
		return nil, gerror.Newf("find grain node failed, kind: %s, id: %s", kind, id)
	}
	return g.spawnGrain(grainInfo.NodeHost, kind, id)
}

// 建议：考虑数据分布，避免热点
func getGrainLocateKey(kind string, id string) string {
	return fmt.Sprintf("actor:%s:%s", kind, id)
}

func (g *grainManager) GetGrainCount(kind string) int {
	grainInfo, ok := g.grainMetas[kind]
	if !ok {
		return 0
	}
	return grainInfo.mgr.Count()
}
