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
	"reflect"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gconv"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	SESSION_ALIVE_INTERVAL = 30 * time.Second
	PERSIST_INTERVAL       = 5 * time.Second
	SINGLE_ALIVE_INTERVAL  = 30 * time.Second
	PUBLIC_UPDATE_INTERVAL = 8 * time.Minute
)

var (
	PersistTick = &gxytimer.Tick{
		Name:     "save_role",
		Interval: PERSIST_INTERVAL,
	}
	SignleAliveOnce = &gxytimer.Once{
		Name:  "signle_alive",
		After: SINGLE_ALIVE_INTERVAL, // 10min 连接断开后存活时间
	}
	PublicUpdateTick = &gxytimer.Tick{
		Name:     "update_role_public",
		Interval: PUBLIC_UPDATE_INTERVAL,
	}
	SessionAliveCheckTick = &gxytimer.Tick{
		Name:     "check_session_alive",
		Interval: SESSION_ALIVE_INTERVAL,
	}
)

type RoleState int32

const (
	RoleStateInit    RoleState = iota
	RoleStateStart             // 角色已初始化，等待登录
	RoleStateLogined           // 角色已登录, 开始正常处理消息
	RoleStateLogout            // 角色已登出
)

type roleModules struct {
	Sign   *RoleSign
	Bag    *RoleBag
	Basic  *RoleBasic
	Public *RolePublic
	Extra  *RoleExtra
	Friend *RoleFriend
}

type RoleMain struct {
	gxymodule.ModuleBase
	roleModules
	*gxyactor.GrainBase
	RoleID int64

	modsHash          map[string]uint64
	session           gxyactor.PID
	state             RoleState
	sessionActiveTime time.Time
}

func NewRoleMain() *RoleMain {
	r := &RoleMain{
		modsHash: map[string]uint64{},
		state:    RoleStateInit,
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

func (r *RoleMain) initRoleModules(ctx context.Context) {
	// 使用反射获取继承了IRoleModule的字段, 并初始化
	modules := &r.roleModules
	t := util.TypeReal(modules)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Type.Kind() != reflect.Ptr {
			continue
		}
		if !field.Type.Implements(reflect.TypeFor[IRoleModule]()) {
			continue
		}
		rmod := util.NewObject(field.Type.Elem())
		reflect.ValueOf(modules).Elem().Field(i).Set(reflect.ValueOf(rmod))
		r.AddModule(ctx, rmod.(IRoleModule))
	}
}

func (r *RoleMain) initModules(ctx context.Context) error {
	r.initRoleModules(ctx)
	if err := r.loadModules(ctx); err != nil {
		return err
	}
	return nil
}

func (r *RoleMain) loadModules(ctx context.Context) error {
	roleID := r.RoleID
	for _, mod := range r.Modules() {
		rmod, _ := mod.(IRoleModule)
		if rmod == nil {
			continue
		}
		modState := rmod.PersistState()
		if modState == nil {
			continue
		}
		if err := loadModuleState(ctx, roleID, modState); err != nil {
			return err
		}
		modState.SetRoleID(roleID)
		r.modsHash[getColName(modState)] = util.GetObjectHash(modState)
	}
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		rmod.SetRole(r)
	}
	return nil
}

func loadModuleState(ctx context.Context, roleID int64, modState IPersistState) error {
	colName := getColName(modState)
	if err := gxymongo.Client().FindOne(ctx, modState, colName, bson.M{"role_id": roleID}); err != nil {
		if err == mongo.ErrNoDocuments {
			// 新创建的文档，设置初始版本号
			modState.SetVersion(0)
		} else {
			return err
		}
	}
	return nil
}

func ensureModIndex(ctx context.Context, modState IPersistState) error {
	colName := getColName(modState)
	if err := gxymongo.Client().EnsureIndexes(ctx, colName, modState.GetIndexes()); err != nil {
		return err
	}
	return nil
}

func (r *RoleMain) HandleMessage(ctx context.Context, msg any) error {
	switch msg := msg.(type) {
	case *pb.ClientMsg:
		pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
		if err != nil {
			return gerror.Wrapf(err, "unmarshal req error, roleID: %d", r.RoleID)
		}
		err = r.handleClientMsg(ctx, msg.Path, pbmsg)
		if err != nil {
			return err
		}
	default:
		glog.Warningf(ctx, "recv unknown msg, roleID: %d, msg: %v", r.RoleID, msg)
	}
	return nil
}

func (r *RoleMain) handleClientMsg(ctx context.Context, path string, msg proto.Message) error {
	r.sessionActiveTime = time.Now()
	switch msg := msg.(type) {
	case *pb.ReqAccountLogin:
		if r.state == RoleStateLogined {
			glog.Warningf(ctx, "role already login, roleID: %d", r.RoleID)
			r.Respond(&pb.Ack{
				Code:   10,
				Path:   path,
				Reason: "role already login",
			})
			return nil
		}
	default:
		if r.state != RoleStateLogined {
			glog.Warningf(ctx, "role not login, roleID: %d, recv msg: %s", r.RoleID, util.FormatObject(msg))
			return nil
		}
	}
	var rsp proto.Message
	res, err := r.HandleProtobufMsg(ctx, msg)
	if err != nil {
		rsp = &pb.Ack{
			Code:   1,
			Path:   path,
			Reason: err.Error(),
		}
	} else if res != nil {
		svrMsg, err := r.newServerMsg(res)
		if err != nil {
			return gerror.Wrapf(err, "send server msg error, roleID: %d", r.RoleID)
		}
		rsp = svrMsg

	}
	if rsp != nil {
		r.Respond(rsp)
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
	r.Timer().AddTick(ctx, PublicUpdateTick, func(ctx context.Context, _ gxytimer.TimerActiveInfo) {
		r.Public.UpdateRolePublic(ctx)
	})
}

func (r *RoleMain) initMsgHandler() {
	for _, mod := range r.Modules() {
		r.AddMsgHandler(mod, "Req")
	}
}

func (r *RoleMain) TickSave(ctx context.Context, _info gxytimer.TimerActiveInfo) {
	if err := r.save(ctx, false); err != nil {
		glog.Errorf(ctx, "save error, roleID: %d, err: %+v", r.RoleID, err)
		// 终止当前进程
		r.Stop(err)
		return
	}
}

func (r *RoleMain) DayRefresh(ctx context.Context, info gxytimer.TimerActiveInfo) {
	r.Sign.SignDayRrefresh(ctx, info)
}

func (r *RoleMain) save(ctx context.Context, force bool) error {
	var errStr string
	client := gxymongo.Client()
	for _, mod := range r.Modules() {
		rmod, _ := mod.(IRoleModule)
		if rmod == nil {
			continue
		}
		modState := rmod.PersistState()
		if modState == nil {
			continue
		}
		colName := getColName(modState)
		// 计算modhash时要除开版本号
		modHash := util.GetObjectHash(modState)
		if modHash == r.modsHash[colName] && !force {
			continue
		}

		// 获取当前版本号用于乐观锁
		currentVersion := modState.GetVersion()

		// 准备更新操作，添加版本号条件
		filter := bson.M{
			"role_id": r.RoleID,
			"version": currentVersion,
		}

		// 设置新版本号为当前时间戳
		modState.SetVersion(currentVersion + 1)
		modState.SetUpdateAt(time.Now())

		// 执行更新操作，只有当版本号匹配时才会成功
		result, err := client.ReplaceOne(ctx, colName, filter, modState,
			options.Replace().SetUpsert(true))
		if err != nil {
			if mongo.IsDuplicateKeyError(err) { // 重复键错误，说明版本号不匹配导致了插入操作失败
				glog.Warning(ctx, result)
				glog.Errorf(ctx, "optimistic lock error: version conflict for mod %s, roleID: %d, currentVersion: %d", colName, r.RoleID, currentVersion)
				return ErrVersionConflict
			}
			errStr += fmt.Sprintf("save mod %s failed: %s", colName, err)
			continue
		}
		glog.Debugf(ctx, "save mod %s success, roleID: %d, version: %d", colName, r.RoleID, modState.GetVersion())

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
	if r.state == RoleStateLogined {
		return nil, gerror.New("role already login")
	}
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
		firstLogin = true
		r.Basic.CreateTm = now
		if err := r.OnRoleCreated(ctx); err != nil {
			return nil, err
		}
	}
	r.Basic.LoginTm = now
	if r.Basic.LoginTm.Sub(r.Basic.LogoutTm).Seconds() < 2*time.Second.Seconds() {
		glog.Infof(ctx, "role reconnect, roleID: %d", r.RoleID)
	}
	r.Timer().Cancel(ctx, SignleAliveOnce.Name)
	r.state = RoleStateLogined
	if err := r.afterRoleLogin(ctx); err != nil {
		return nil, err
	}
	return &pb.RspAccountLogin{
		FirstLogin: firstLogin,
	}, nil
}

func (r *RoleMain) OnRoleCreated(ctx context.Context) error {
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		rmod.OnCreate(ctx)
	}
	r.Public.UpdateRolePublic(ctx)
	// 建号强制保存一次
	if err := r.save(ctx, true); err != nil {
		return err
	}
	return nil
}

func (r *RoleMain) afterRoleLogin(ctx context.Context) error {
	r.sessionActiveTime = time.Now()
	r.Timer().AddTick(ctx, SessionAliveCheckTick, func(ctx context.Context, _info gxytimer.TimerActiveInfo) {
		r.checkSessionAlive(ctx)
	})
	return nil
}

func (r *RoleMain) checkSessionAlive(ctx context.Context) {
	if time.Since(r.sessionActiveTime) <= SESSION_ALIVE_INTERVAL {
		return
	}
	r.dologout(ctx, "session alive timeout")
}

func (r *RoleMain) ReqAccountLogout(ctx context.Context, req *pb.ReqAccountLogout) error {
	sender := r.Sender()
	if !gxyactor.PidEqual(sender, r.session) {
		return nil
	}
	return r.dologout(ctx, req.Reason)
}

func (r *RoleMain) dologout(ctx context.Context, reason string) error {
	if r.state == RoleStateLogout {
		return nil
	}
	r.Timer().Cancel(ctx, SessionAliveCheckTick.Name)
	r.session = nil
	r.Basic.LogoutTm = time.Now()
	r.Public.UpdateRolePublic(ctx)
	if err := r.save(ctx, false); err != nil {
		return err
	}
	r.Timer().AddOnce(ctx, SignleAliveOnce, func(ctx context.Context, _info gxytimer.TimerActiveInfo) {
		r.Stop(errors.New("single alive timeout"))
	})
	r.state = RoleStateLogout
	glog.Infof(ctx, "role logout, roleID: %d, reason %s", r.RoleID, reason)
	return nil
}

func (r *RoleMain) Terminate(ctx context.Context, err error) {
	if serr := r.StopModule(ctx); serr != nil {
		glog.Errorf(ctx, "stop module error, roleID: %d, err: %v", r.RoleID, err)
	}
	if serr := r.save(ctx, true); serr != nil {
		glog.Errorf(ctx, "save error, roleID: %d, err: %+v", r.RoleID, serr)
	}
	glog.Infof(ctx, "role actor terminate, roleID: %d, reason: %v", r.RoleID, err)
}

func (r *RoleMain) GetRolePublic(ctx context.Context) *pb.PRolePublic {
	return &pb.PRolePublic{
		RoleId:     r.RoleID,
		Name:       r.Basic.RoleName,
		Head:       r.Basic.Head,
		CreateTime: r.Basic.CreateTm.Unix(),
	}
}
