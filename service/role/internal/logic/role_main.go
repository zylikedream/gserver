package logic

import (
	"context"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxylocator"
	"gserver/core/gxymodule"
	"gserver/core/gxymongo"
	"gserver/core/gxynet/message"
	"gserver/util"
	"time"

	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	ROLE_MSG_INIT = "init_role"
)

type RoleMainOption struct {
	Locator *gxylocator.Locator
}

type RoleMain struct {
	gxymodule.Module
	gxyactor.ActorBase
	RoleID int64

	Sign  *RoleSign
	Bag   *RoleBag
	Basic *RoleBasic

	timer      *gxyactor.ActorTimer
	modsHash   map[string]uint64
	msgHandler *util.MsgHandler
	reason     string

	opt     RoleMainOption
	session gen.PID
}

func NewRoleMain(opt RoleMainOption) *RoleMain {
	return &RoleMain{
		modsHash:   map[string]uint64{},
		msgHandler: util.NewMsgHandler(),
		opt:        opt,
	}
}

func (r *RoleMain) Init(args ...any) error {
	Reason, _ := args[0].(string)
	RoleID, _ := args[1].(int64)
	r.reason = Reason
	r.RoleID = RoleID
	if err := r.registerRole(RoleID); err != nil {
		return err
	}
	ctx := context.Background()
	r.Sign = NewRoleSign()
	r.AddModule(ctx, r.Sign)

	r.Bag = NewRoleBag()
	r.AddModule(ctx, r.Bag)

	r.Basic = NewRoleBasic()
	r.AddModule(ctx, r.Basic)

	r.Send(r.PID(), gxyactor.NewActorMessage(ROLE_MSG_INIT, ""))
	return nil
}

func (r *RoleMain) OnHandleMessage(from gen.PID, msg gxyactor.ActorMessage) error {
	switch msg.Name {
	case ROLE_MSG_INIT:
		if err := r.initRole(); err != nil {
			return err
		}
	case gxyactor.MsgTimer:
		data, ok := msg.Data.(*gxyactor.TimerMsg)
		if !ok {
			glog.Errorf(context.Background(), "unknown message data type: %T", msg.Data)
			return nil
		}
		data.Fun(context.Background(), data.Time)
	case gxyactor.MsgClientReq:
		if r.session != from {
			glog.Errorf(context.Background(), "unknown message data from, session: %v, from: %v", r.session, from)
			return nil
		}
		req, ok := msg.Data.(message.Message)
		if !ok {
			glog.Errorf(context.Background(), "unknown message data type: %T", msg.Data)
			return nil
		}
		methodName := req.Path
		meta := r.msgHandler.GetMethodMeta(methodName)
		if meta == nil {
			return gerror.Newf("no method meta (%s)", methodName)
		}
		rsp, err := r.msgHandler.CallWithMsg(context.Background(), methodName, req.Msg)
		if err != nil {
			glog.Errorf(context.Background(), "handle call error, roleID: %d, method: %s, err: %v", r.RoleID, req.Path, err)
			return nil
		}
		if rsp == nil {
			return nil
		}
		if err := r.Send(from, gxyactor.NewActorMessage(gxyactor.MsgServerRsp, rsp)); err != nil {
			return err
		}
		return nil
	default:
		glog.Errorf(context.Background(), "unknown message type: %s", msg.Name)
	}
	return nil
}

func (r *RoleMain) initRole() error {
	if err := r.initRoleModules(); err != nil {
		return err
	}
	r.timer = gxyactor.NewActorTimer(r.PID())
	r.timer.AddTick(context.Background(), &gxyactor.Tick{
		Tick: 1 * time.Second,
	}, func(ctx context.Context, tm time.Time) {
		r.save(ctx, false)
	})
	r.msgHandler.AddHandler(r)
	for _, mod := range r.Modules() {
		r.msgHandler.AddHandler(mod)
	}
	return nil
}

func (r *RoleMain) initRoleModules() error {
	roleID := r.RoleID
	ctx := context.Background()
	for _, mod := range r.Modules() {
		if err := gxymongo.Client().FindOne(ctx, roleID, mod.GetName(), mod); err != nil {
			if err != mongo.ErrNoDocuments {
				return err
			}
		}
		r.modsHash[mod.GetName()] = util.GetObjectHash(mod)
	}
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		if err := rmod.AfterInit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *RoleMain) save(ctx context.Context, force bool) error {
	var errStr string
	client := gxymongo.Client()
	for _, mod := range r.Modules() {
		modName := mod.GetName()
		modHash := util.GetObjectHash(mod)
		if modHash == r.modsHash[modName] && !force {
			continue
		}
		if _, err := client.ReplaceOne(ctx, modName, bson.M{"_id": r.RoleID},
			mod, options.Replace().SetUpsert(true)); err != nil {
			errStr += fmt.Sprintf("save mod %s failed: %s", modName, err)
			continue
		}
		r.modsHash[modName] = modHash
	}
	if errStr != "" {
		return errors.New(errStr)
	}
	return nil
}

func (r *RoleMain) Terminate(reason error) {
	if err := r.unRegisterRole(r.RoleID); err != nil {
		glog.Errorf(context.Background(), "unregister role failed: %s", err)
	}
	if err := r.save(context.Background(), true); err != nil {
		glog.Errorf(context.Background(), "save role failed: %s", err)
	}
}

func (r *RoleMain) registerRole(roleID int64) error {
	return r.opt.Locator.Register(context.Background(), fmt.Sprintf("%d", roleID),
		gxyactor.ActorSystem().GetNodeName())
}

func (r *RoleMain) unRegisterRole(roleID int64) error {
	return r.opt.Locator.Unregister(context.Background(), fmt.Sprintf("%d", roleID))
}
