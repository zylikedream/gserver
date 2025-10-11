package logic

import (
	"context"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxymongo"
	"gserver/protocol/pb"
	"gserver/util"
	"strconv"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/text/gstr"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	PersistTick = &gxyactor.Tick{
		Name: "save_role",
		Tick: 5 * time.Second,
	}
	SignleAliveOnce = &gxyactor.Once{
		Name:    "signle_alive",
		EndTime: 10 * time.Minute, // 10min 连接断开后存活时间
	}
)

type RoleState int32

const (
	RoleStateInit RoleState = iota
	RoleStateStart
	RoleStateLogin
	RoleStateLogout
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
	ctx     context.Context
	state   RoleState
}

func NewRoleMain() *RoleMain {
	r := &RoleMain{
		modsHash:   map[string]uint64{},
		msgHandler: util.NewMsgHandler("Req"),
		ctx:        gxylog.NewContext(context.Background(), "role"),
		state:      RoleStateInit,
	}
	r.SetSelf(r)
	return r
}

func (r *RoleMain) Init(actx actor.Context) error {
	var err error
	r.RoleID, err = strconv.ParseInt(actx.Self().Id, 10, 64)
	if err != nil {
		return err
	}
	r.pid = actx.Self()
	r.Sign = NewRoleSign()
	r.ctx = gxylog.WithValue(r.ctx, gxylog.ContextKeyRoleID, r.RoleID)
	r.AddModule(r.ctx, r.Sign)

	r.Bag = NewRoleBag()
	r.AddModule(r.ctx, r.Bag)

	r.Basic = NewRoleBasic()
	r.AddModule(r.ctx, r.Basic)

	if err = r.initRole(); err != nil {
		return err
	}
	r.initTimer()
	r.initMsgHandler()
	r.state = RoleStateStart
	return nil
}
func (r *RoleMain) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		if err := r.Init(ctx); err != nil {
			glog.Errorf(r.ctx, "init role error, roleID: %d, err: %v", r.RoleID, err)
			return
		}
	case *gxyactor.ActorTimerMsg:
		r.timer.Active(r.ctx, msg)
	case *pb.ClientMsg:
		pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
		if err != nil {
			glog.Errorf(r.ctx, "unmarshal req error, roleID: %d, err: %v", r.RoleID, err)
			return
		}
		if r.state != RoleStateLogin {
			if _, ok := pbmsg.(*pb.ReqAccountLogin); !ok {
				glog.Errorf(r.ctx, "role not login, roleID: %d, recv msg: %v", r.RoleID, pbmsg.ProtoReflect().Descriptor().Name())
				return
			}
		}
		r.actx = ctx
		var rsp proto.Message
		tm := time.Now()
		glog.Debugf(r.ctx, "recv client msg, args: %v", pbmsg)
		result, err := r.msgHandler.CallWithMsg(r.ctx, pbmsg)
		glog.Debugf(r.ctx, "handle call result, args: %v, result: %v, cost: %vms", pbmsg, result, time.Since(tm).Milliseconds())
		if err != nil {
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

		if ctx.Sender() != nil && rsp != nil {
			svrMsg, err := r.newServerMsg(rsp)
			if err != nil {
				glog.Errorf(r.ctx, "send server msg error, roleID: %d, err: %v", r.RoleID, err)
				return
			}
			ctx.Respond(svrMsg)
		}
	default:
		glog.Errorf(r.ctx, "unknown message type: %v", msg)
	}
}

func (r *RoleMain) newServerMsg(msg proto.Message) (*pb.ServerMsg, error) {
	rspMsg := &pb.ServerMsg{
		Msg: &anypb.Any{},
	}
	if err := anypb.MarshalFrom(rspMsg.Msg, msg, proto.MarshalOptions{}); err != nil {
		return nil, err
	}
	return rspMsg, nil
}

func (r *RoleMain) initRole() error {
	if err := r.initRoleModules(); err != nil {
		return err
	}
	return nil
}

func (r *RoleMain) initTimer() {
	r.timer = gxyactor.NewActorTimer(r.pid)
	r.timer.AddTick(r.ctx, PersistTick, r.TickSave)
}

func (r *RoleMain) initMsgHandler() {
	r.msgHandler.AddHandler(r)
	for _, mod := range r.Modules() {
		r.msgHandler.AddHandler(mod)
	}
}

func (r *RoleMain) initRoleModules() error {
	roleID := r.RoleID
	created := false
	for _, mod := range r.Modules() {
		colName := gstr.CaseSnake(mod.GetName())
		if err := gxymongo.Client().FindOne(r.ctx, mod, colName, bson.M{"role_id": roleID}); err != nil {
			if err != mongo.ErrNoDocuments {
				return err
			} else {
				created = true
			}
		}
		r.modsHash[colName] = util.GetObjectHash(mod)
	}
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		if err := rmod.AfterInit(r.ctx); err != nil {
			return err
		}
	}
	if created {
		if err := r.save(r.ctx, true); err != nil {
			return err
		}
	}
	return nil
}

func (r *RoleMain) TickSave(ctx context.Context, _ time.Time) {
	r.save(ctx, false)
}

func (r *RoleMain) save(ctx context.Context, force bool) error {
	var errStr string
	client := gxymongo.Client()
	for _, mod := range r.Modules() {
		colName := gstr.CaseSnake(mod.GetName())
		modHash := util.GetObjectHash(mod)
		if modHash == r.modsHash[colName] && !force {
			continue
		}
		if _, err := client.ReplaceOne(ctx, colName, bson.M{"role_id": r.RoleID},
			mod, options.Replace().SetUpsert(true)); err != nil {
			errStr += fmt.Sprintf("save mod %s failed: %s", colName, err)
			continue
		}
		r.modsHash[colName] = modHash
	}
	if errStr != "" {
		return errors.New(errStr)
	}
	return nil
}

func (r *RoleMain) SendClient(msg proto.Message) {
	if r.session == nil {
		glog.Errorf(r.ctx, "session is nil, roleID: %d", r.RoleID)
		return
	}
	svrMsg, err := r.newServerMsg(msg)
	if err != nil {
		glog.Errorf(r.ctx, "new server msg error, roleID: %d, err: %v", r.RoleID, err)
		return
	}
	gxyactor.ActorSystem().Send(r.session, svrMsg)
}

func (r *RoleMain) ReqAccountLogin(ctx context.Context, req *pb.ReqAccountLogin) (*pb.RspAccountLogin, error) {
	if r.session != nil && !gxyactor.PidEqual(r.session, r.actx.Sender()) { // 表示重复登录
		// 断开旧连接
		r.actx.Send(r.session, &pb.ActorStop{
			Reason: "duplicate login",
		})
	}
	r.session = r.actx.Sender()
	firstLogin := false
	now := time.Now()
	if r.Basic.CreateTm.IsZero() {
		r.Basic.CreateTm = now
		firstLogin = true
	}
	r.Basic.LoginTm = now
	if r.Basic.LoginTm.Sub(r.Basic.LogoutTm).Seconds() < 2*time.Second.Seconds() {
		glog.Infof(ctx, "role reconnect, roleID: %d", r.RoleID)
	}
	r.timer.Cancel(SignleAliveOnce.Name)
	r.state = RoleStateLogin
	return &pb.RspAccountLogin{
		FirstLogin: firstLogin,
	}, nil
}

func (r *RoleMain) ReqAccountLogout(ctx context.Context, req *pb.ReqAccountLogout) error {
	sender := r.actx.Sender()
	if !gxyactor.PidEqual(sender, r.session) {
		return nil
	}
	glog.Infof(ctx, "role logout, roleID: %d", r.RoleID)
	r.session = nil
	r.Basic.LogoutTm = time.Now()
	r.save(ctx, false)
	r.timer.AddOnce(ctx, SignleAliveOnce, func(ctx context.Context, _ time.Time) {
		r.actx.Stop(r.pid)
	})
	r.state = RoleStateLogout
	return nil
}

func (r *RoleMain) Stop(ctx context.Context) {
	r.save(ctx, true)
}
