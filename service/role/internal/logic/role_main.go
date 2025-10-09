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

func (r *RoleMain) Recive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		if err := r.Init(ctx); err != nil {
			glog.Errorf(context.Background(), "init role error, roleID: %d, err: %v", r.RoleID, err)
			return
		}
	case *gxyactor.ActorTimerMsg:
		r.timer.Active(context.Background(), msg)
	case *pb.ReqHandShake:
		r.session = ctx.Sender()
	case *pb.ClientMsg:
		pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
		if err != nil {
			glog.Errorf(context.Background(), "unmarshal req error, roleID: %d, err: %v", r.RoleID, err)
			return
		}
		rsp, err := r.msgHandler.CallWithMsg(context.Background(), pbmsg)
		if err != nil {
			glog.Errorf(context.Background(), "handle call error, roleID: %d, args: %v, err: %v", r.RoleID, pbmsg, err)
			return
		}
		if rsp == nil {
			return
		}

		pbRsp, ok := rsp.(proto.Message)
		if !ok {
			glog.Errorf(context.Background(), "rsp is not proto.Message, roleID: %d, rsp: %v", r.RoleID, rsp)
			return
		}
		if ctx.Sender() != nil {
			svrMsg, err := r.newServerMsg(pbRsp)
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
