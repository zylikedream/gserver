package logic

import (
	"context"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxymodule"
	"gserver/core/gxymongo"
	"gserver/protocol/pb"
	"gserver/util"
	"strconv"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type RoleMain struct {
	gxymodule.Module
	RoleID int64

	Sign  *RoleSign
	Bag   *RoleBag
	Basic *RoleBasic

	timer      *gxyactor.ActorTimer
	modsHash   map[string]uint64
	msgHandler *util.MsgHandler
	pid        gxyactor.PID

	session gxyactor.PID
	actx    actor.Context
}

func NewRoleMain() *RoleMain {
	return &RoleMain{
		modsHash:   map[string]uint64{},
		msgHandler: util.NewMsgHandler(),
	}
}

func (r *RoleMain) Init(actx actor.Context) error {
	var err error
	r.RoleID, err = strconv.ParseInt(actx.Self().Id, 10, 64)
	if err != nil {
		return err
	}
	r.pid = actx.Self()
	ctx := context.Background()
	r.Sign = NewRoleSign()
	r.AddModule(ctx, r.Sign)

	r.Bag = NewRoleBag()
	r.AddModule(ctx, r.Bag)

	r.Basic = NewRoleBasic()
	r.AddModule(ctx, r.Basic)

	if err = r.initRole(); err != nil {
		return err
	}
	return nil
}

func (r *RoleMain) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		if err := r.Init(ctx); err != nil {
			glog.Errorf(context.Background(), "init role error, roleID: %d, err: %v", r.RoleID, err)
			return
		}
	case *gxyactor.ActorTimerMsg:
		r.timer.Active(context.Background(), msg)
	case *pb.ClientMsg:
		pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
		if err != nil {
			glog.Errorf(context.Background(), "unmarshal req error, roleID: %d, err: %v", r.RoleID, err)
			return
		}
		r.actx = ctx
		var rsp proto.Message
		result, err := r.msgHandler.CallWithMsg(context.Background(), pbmsg)
		if err != nil {
			glog.Errorf(context.Background(), "handle call error, roleID: %d, args: %v, err: %v", r.RoleID, pbmsg, err)
			rsp = &pb.Ack{
				Code:   1,
				Path:   msg.Path,
				Reason: err.Error(),
			}
		} else if result != nil {
			rsp1, ok := result.(proto.Message)
			if ok {
				rsp = rsp1
			}
		}

		if ctx.Sender() != nil {
			svrMsg, err := r.newServerMsg(rsp)
			if err != nil {
				glog.Errorf(context.Background(), "send server msg error, roleID: %d, err: %v", r.RoleID, err)
				return
			}
			ctx.Respond(svrMsg)
		}
	default:
		glog.Errorf(context.Background(), "unknown message type: %v", msg)
	}
}

func (r *RoleMain) newServerMsg(msg proto.Message) (*pb.ServerMsg, error) {
	rspMsg := &pb.ServerMsg{}
	if err := anypb.MarshalFrom(rspMsg.Msg, msg, proto.MarshalOptions{}); err != nil {
		return nil, err
	}
	return rspMsg, nil
}

func (r *RoleMain) initRole() error {
	if err := r.initRoleModules(); err != nil {
		return err
	}
	r.timer = gxyactor.NewActorTimer(r.pid)
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
	created := false
	for _, mod := range r.Modules() {
		if err := gxymongo.Client().FindOne(ctx, roleID, mod.GetName(), mod); err != nil {
			if err != mongo.ErrNoDocuments {
				return err
			} else {
				created = true
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
	if created {
		if err := r.save(ctx, true); err != nil {
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

func (r *RoleMain) SendClient(msg proto.Message) {
	ctx := context.Background()
	if r.session == nil {
		glog.Errorf(ctx, "session is nil, roleID: %d", r.RoleID)
		return
	}
	svrMsg, err := r.newServerMsg(msg)
	if err != nil {
		glog.Errorf(ctx, "new server msg error, roleID: %d, err: %v", r.RoleID, err)
		return
	}
	gxyactor.ActorSystem().Send(r.session, svrMsg)
}

func (r *RoleMain) ReqAccountLogin(ctx context.Context, req *pb.ReqAccountLogin) (*pb.RspAccountLogin, error) {
	r.session = r.actx.Sender()
	firstLogin := false
	now := time.Now()
	if r.Basic.CreateTm.IsZero() {
		r.Basic.CreateTm = now
		firstLogin = true
	}
	r.Basic.LoginTm = now
	return &pb.RspAccountLogin{
		FirstLogin: firstLogin,
	}, nil
}
