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
	gxymodule.ModuleBase
	*gxyactor.GrainBase
	RoleID int64
	Sign   *RoleSign
	Bag    *RoleBag
	Basic  *RoleBasic
	Extra  *RoleExtra

	modsHash   map[string]uint64
	msgHandler *util.MsgHandler
	session    gxyactor.PID
	state      RoleState
}

func NewRoleMain() *RoleMain {
	r := &RoleMain{
		modsHash:   map[string]uint64{},
		msgHandler: util.NewMsgHandler("Req"),
		state:      RoleStateInit,
	}
	ctx := gxylog.NewContext(context.Background(), "role")
	r.GrainBase = gxyactor.NewGrainBase(ctx, r)
	return r
}

func (r *RoleMain) Init(ctx context.Context) error {
	r.RoleID = gconv.Int64(r.GrainID())
	if r.RoleID == 0 {
		return gerror.Newf("roleID is invalid, roleID: %s", r.GrainID())
	}
	r.SetLogValue(gxylog.ContextKeyRoleID, r.RoleID)
	// 验证角色账号是否存在
	if account := GetAccountByRoleID(r.RoleID); account == "" {
		return gerror.Newf("role account not exist, roleID: %d", r.RoleID)
	}

	return nil
}

func (r *RoleMain) DelayInit(ctx context.Context) error {
	if err := r.initRole(ctx); err != nil {
		return gerror.Wrapf(err, "init role error, roleID: %d", r.RoleID)
	}
	if err := r.afterInitRole(ctx); err != nil {
		return gerror.Wrapf(err, "after init role error, roleID: %d", r.RoleID)
	}
	glog.Debugf(ctx, "role start success, roleID: %d", r.RoleID)
	return nil
}

func (r *RoleMain) initRole(ctx context.Context) error {
	if err := r.initModules(ctx); err != nil {
		return err
	}
	r.initTimer(ctx)
	r.initMsgHandler()
	RoleMgr().Add(r.RoleID, r.Self())
	r.state = RoleStateStart
	glog.Debugf(ctx, "init role success, roleID: %d", r.RoleID)
	return nil
}

func (r *RoleMain) afterInitRole(ctx context.Context) error {
	if err := r.StartModule(ctx); err != nil {
		return err
	}
	r.Timer().RestoreCron(ctx)
	return nil
}

func (r *RoleMain) initModules(ctx context.Context) error {
	r.Sign = NewRoleSign()
	r.AddModule(ctx, r.Sign)

	r.Bag = NewRoleBag()
	r.AddModule(ctx, r.Bag)

	r.Basic = NewRoleBasic()
	r.AddModule(ctx, r.Basic)

	r.Extra = NewRoleExtra()
	r.AddModule(ctx, r.Extra)

	if err := r.loadModules(ctx); err != nil {
		return err
	}
	return nil
}

func (r *RoleMain) loadModules(ctx context.Context) error {
	roleID := r.RoleID
	for _, mod := range r.Modules() {
		colName := getModuleColName(mod)
		if err := gxymongo.Client().FindOne(ctx, mod, colName, bson.M{"role_id": roleID}); err != nil {
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
	return gstr.CaseSnake(mod.GetModName())
}

func (r *RoleMain) HandleMessage(ctx context.Context, msg any) error {
	switch msg := msg.(type) {
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
		var rsp proto.Message
		tm := time.Now()
		glog.Debugf(ctx, "recv client msg, args: %v", pbmsg)
		result, err := r.msgHandler.CallWithMsg(ctx, pbmsg)
		if err != nil {
			rsp = &pb.Ack{
				Code:   1,
				Path:   msg.Path,
				Reason: err.Error(),
			}
			glog.Debugf(ctx, "handle call result error, args: %s, error: %+v, cost: %vms", util.ForamtProto(pbmsg), result, time.Since(tm).Milliseconds())
		} else if result != nil {
			rsp1, ok := result.(proto.Message)
			if ok {
				rsp = rsp1
			}
			glog.Infof(ctx, "handle call result succ, args: %s, result: %s, cost: %vms",
				util.ForamtProto(pbmsg), util.ForamtProto(rsp), time.Since(tm).Milliseconds())
		}

		if rsp != nil {
			svrMsg, err := r.newServerMsg(rsp)
			if err != nil {
				return gerror.Wrapf(err, "send server msg error, roleID: %d", r.RoleID)
			}

			r.Respond(svrMsg)
		}
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

func (r *RoleMain) initTimer(ctx context.Context) {
	r.Timer().SetCronState(r.Extra)
	r.Timer().AddTick(ctx, PersistTick, r.TickSave)
	r.Timer().AddCron(ctx, gxytimer.DayRefresh, r.DayRefresh)
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
		colName := gstr.CaseSnake(mod.GetModName())
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

func (r *RoleMain) SendClient(ctx context.Context, msg proto.Message) {
	if r.session == nil {
		glog.Errorf(ctx, "session is nil, roleID: %d", r.RoleID)
		return
	}
	svrMsg, err := r.newServerMsg(msg)
	if err != nil {
		glog.Errorf(ctx, "new server msg error, roleID: %d, err: %v", r.RoleID, err)
		return
	}
	r.Send(r.session, svrMsg)
}

func (r *RoleMain) ReqAccountLogin(ctx context.Context, req *pb.ReqAccountLogin) (*pb.RspAccountLogin, error) {
	sender := r.Sender()
	if r.session != nil && !gxyactor.PidEqual(r.session, sender) { // 表示重复登录
		// 断开旧连接
		r.Send(r.session, &pb.ActorStop{
			Reason: "duplicate login",
		})
	}
	r.session = sender
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
	r.Timer().Cancel(SignleAliveOnce.Name)
	r.state = RoleStateLogin
	return &pb.RspAccountLogin{
		FirstLogin: firstLogin,
	}, nil
}

func (r *RoleMain) ReqAccountLogout(ctx context.Context, req *pb.ReqAccountLogout) error {
	sender := r.Sender()
	if !gxyactor.PidEqual(sender, r.session) {
		return nil
	}
	glog.Infof(ctx, "role logout, roleID: %d", r.RoleID)
	r.session = nil
	r.Basic.LogoutTm = time.Now()
	r.save(ctx, false)
	r.Timer().AddOnce(ctx, SignleAliveOnce, func(ctx context.Context, _info gxytimer.TimerActiveInfo) {
		r.Stop(errors.New("single alive timeout"))
	})
	r.state = RoleStateLogout
	return nil
}

func (r *RoleMain) Terminate(ctx context.Context, err error) {
	glog.Infof(ctx, "role actor terminate, roleID: %d, reason: %v", r.RoleID, err)
	r.save(ctx, true)
}
