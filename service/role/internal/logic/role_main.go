package logic

import (
	"context"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxymongo"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"
	"gserver/util"
	"time"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/text/gstr"
	"github.com/gogf/gf/v2/util/gconv"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	PersistTick = &gxytimer.Tick{
		Name:     "save_role",
		Interval: 5 * time.Second,
	}
	SignleAliveOnce = &gxytimer.Once{
		Name:  "signle_alive",
		After: 10 * time.Minute, // 10min 连接断开后存活时间
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
	Extra *RoleExtra

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

func (r *RoleMain) init(actx actor.Context) error {
	r.pid = actx.Self()
	gctx, ok := actx.(*gxyactor.GrainContext)
	if !ok {
		return gerror.Newf("actor context is not grain context, roleID: %d", r.RoleID)
	}
	strRoleID := gctx.MetadataValue(gxyactor.CONTEXT_KEY_ID).(string)
	r.RoleID = gconv.Int64(strRoleID)
	if r.RoleID == 0 {
		return gerror.Newf("roleID is invalid, roleID: %s", strRoleID)
	}
	r.ctx = gxylog.WithValue(r.ctx, gxylog.ContextKeyRoleID, r.RoleID)
	// 验证角色账号是否存在
	if account := GetAccountByRoleID(r.RoleID); account == "" {
		return gerror.Newf("role account not exist, roleID: %d", r.RoleID)
	}

	actx.Send(r.pid, &gxyactor.ActorInitMsg{})

	return nil
}

func (r *RoleMain) initRole() error {
	if err := r.initModules(); err != nil {
		return err
	}
	r.initTimer()
	r.initMsgHandler()
	RoleMgr().Add(r.RoleID, r.pid)
	r.state = RoleStateStart
	glog.Debugf(r.ctx, "init role success, roleID: %d", r.RoleID)
	return nil
}

func (r *RoleMain) afterInit() error {
	if err := r.Start(r.ctx); err != nil {
		return err
	}
	r.timer.RestoreCron(r.ctx)
	return nil
}

func (r *RoleMain) initModules() error {
	r.Sign = NewRoleSign()
	r.AddModule(r.ctx, r.Sign)

	r.Bag = NewRoleBag()
	r.AddModule(r.ctx, r.Bag)

	r.Basic = NewRoleBasic()
	r.AddModule(r.ctx, r.Basic)

	r.Extra = NewRoleExtra()
	r.AddModule(r.ctx, r.Extra)

	if err := r.loadModules(); err != nil {
		return err
	}
	return nil
}

func (r *RoleMain) loadModules() error {
	roleID := r.RoleID
	for _, mod := range r.Modules() {
		colName := getModuleColName(mod)
		if err := gxymongo.Client().FindOne(r.ctx, mod, colName, bson.M{"role_id": roleID}); err != nil {
			if err != mongo.ErrNoDocuments {
				return err
			}
		}
		r.modsHash[colName] = util.GetObjectHash(mod)
	}
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		rmod.SetRole(r)
	}
	return nil
}

func getModuleColName(mod gxymodule.IModule) string {
	return gstr.CaseSnake(mod.GetName())
}
func (r *RoleMain) Receive(ctx actor.Context) {
	if err := r.doReceive(ctx); err != nil {
		glog.Errorf(r.ctx, "%+v", err)
		ctx.Stop(r.pid)
		return
	}
}

func (r *RoleMain) doReceive(ctx actor.Context) error {
	var err error
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		if err = r.init(ctx); err != nil {
			return gerror.Wrapf(err, "init error, roleID: %d", r.RoleID)
		}
		if err := r.afterInit(); err != nil {
			return gerror.Wrapf(err, "after init role error, roleID: %d", r.RoleID)
		}
	case *gxyactor.ActorInitMsg:
		if err := r.initRole(); err != nil {
			return gerror.Wrapf(err, "init role error, roleID: %d", r.RoleID)
		}
	case gxyactor.ActorTimerMsg:
		r.timer.Active(r.ctx, msg)
	case *pb.ClientMsg:
		pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
		if err != nil {
			return gerror.Wrapf(err, "unmarshal req error, roleID: %d", r.RoleID)
		}
		if r.state != RoleStateLogin {
			if _, ok := pbmsg.(*pb.ReqAccountLogin); !ok {
				return gerror.Newf("role not login, roleID: %d, recv msg: %v", r.RoleID, pbmsg.ProtoReflect().Descriptor().Name())
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
				return gerror.Wrapf(err, "send server msg error, roleID: %d", r.RoleID)
			}

			ctx.Respond(svrMsg)
		}
	case *actor.Stopped:
		r.Stop()
	}
	return nil

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

func (r *RoleMain) initTimer() {
	r.timer = gxyactor.NewActorTimer(r.pid, r.Extra)
	r.timer.AddTick(r.ctx, PersistTick, r.TickSave)
	r.timer.AddCron(r.ctx, gxytimer.DayRefresh, r.DayRefresh)
}

func (r *RoleMain) initMsgHandler() {
	r.msgHandler.AddHandler(r)
	for _, mod := range r.Modules() {
		r.msgHandler.AddHandler(mod)
	}
}

func (r *RoleMain) TickSave(ctx context.Context, _info gxytimer.TimerActiveInfo) {
	r.save(ctx, false)
}

func (r *RoleMain) DayRefresh(ctx context.Context, info gxytimer.TimerActiveInfo) {
	r.Sign.SignDayRrefresh(ctx, info)
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
	r.timer.AddOnce(ctx, SignleAliveOnce, func(ctx context.Context, _info gxytimer.TimerActiveInfo) {
		r.actx.Stop(r.pid)
	})
	r.state = RoleStateLogout
	return nil
}

func (r *RoleMain) Stop() {
	glog.Infof(r.ctx, "role actor stop, roleID: %d", r.RoleID)
	r.timer.CancelAll()
	r.save(r.ctx, true)
}
