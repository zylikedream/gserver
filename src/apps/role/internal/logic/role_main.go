package logic

import (
	"context"
	"errors"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxypgx"
	"gserver/core/gxytimer"
	"gserver/core/gxyutil"
	"gserver/gameconfig"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"reflect"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gconv"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"gorm.io/gorm"
)

const (
	SESSION_ALIVE_INTERVAL = 10 * time.Minute
	PERSIST_INTERVAL       = 600 * time.Second
	SINGLE_ALIVE_INTERVAL  = 10 * time.Minute
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
	RoleStateLoad              // 角色已初始化，等待登录
	RoleStateLogined           // 角色已登录, 开始正常处理消息
	RoleStateLogout            // 角色已登出
)

type roleModules struct {
	Bag    *RoleBag
	Basic  *RoleBasic
	Public *RolePublic
	Extra  *RoleExtra
}

type RoleMain struct {
	gxymodule.ModuleBase
	roleModules
	*gxyactor.ActorBase
	RoleID int64

	modsHash          map[string]uint64
	session           gxyactor.PID
	state             RoleState
	sessionActiveTime time.Time
	eventBus          event.IEventBus
}

func NewRoleMain() *RoleMain {
	r := &RoleMain{
		modsHash: map[string]uint64{},
		state:    RoleStateInit,
	}
	ctx := gxylog.NewContext(context.Background(), "role")
	r.ActorBase = gxyactor.NewActorBase(ctx, r)
	return r
}

func (r *RoleMain) Init(ctx context.Context, args []any) error {
	if len(args) == 0 {
		return gerror.New("roleID is required in init args")
	}
	r.RoleID = gconv.Int64(args[0])
	if r.RoleID == 0 {
		return gerror.Newf("roleID is invalid, roleID: %v", args[0])
	}
	r.SetLogValue(gxylog.ContextKeyRoleID, r.RoleID)
	// 验证角色账号是否存在
	if account := GetAccountByRoleID(r.RoleID); account == "" {
		return gerror.Newf("role account not exist, roleID: %d", r.RoleID)
	}

	return nil
}

func (r *RoleMain) DelayInit(ctx context.Context) error {
	r.eventBus = event.NewEventBus()
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
	r.state = RoleStateLoad
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
	t := gxyutil.TypeReal(modules)
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
		rmod := gxyutil.NewObject(field.Type.Elem())
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
	}
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		rmod.SetRole(r)
	}
	return nil
}

type tabler interface {
	TableName() string
}

func loadModuleState(_ context.Context, roleID int64, modState IPersistState) error {
	tableName := modState.(tabler).TableName()
	err := gxypgx.DB().Table(tableName).Where("role_id = ?", roleID).First(modState).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		modState.SetRoleID(roleID)
		return nil
	}
	return err
}

func canHandleMsg(state RoleState, msg proto.Message) bool {
	if _, ok := msg.(*pb.ReqAccountLogin); ok {
		if state != RoleStateInit { // 只有init状态下不能处理登录消息
			return true
		}
		return false
	}
	// 普通消息只有login状态下才能处理,
	if state != RoleStateLogined {
		return false
	}
	return true
}

func (r *RoleMain) HandleMessage(ctx context.Context, msg any) error {
	glog.Debugf(ctx, "handle role msg, msg: %s", gxyutil.FormatObject(msg))
	_, err := r.AutoHandleMsg(ctx, msg)
	return err
}

func (r *RoleMain) HandleClientMsg(ctx context.Context, climsg *pb.ClientMsg) (proto.Message, error) {
	id := climsg.Id
	pbmsg, err := anypb.UnmarshalNew(climsg.GetMsg(), proto.UnmarshalOptions{})
	if err != nil {
		return nil, gerror.Wrapf(err, "unmarshal req error, roleID: %d", r.RoleID)
	}
	r.sessionActiveTime = time.Now()
	if !canHandleMsg(r.state, pbmsg) {
		glog.Warningf(ctx, "role recv msg in state %d, ignore it  msg: %s", r.state, gxyutil.FormatObject(pbmsg))
		return nil, nil
	}
	var rsp proto.Message
	res, err := r.DoCallMsgHandler(ctx, pbmsg)
	if err != nil {
		res = &pb.Ack{
			Code:   1,
			Id:     id,
			Reason: err.Error(),
		}
	}
	if res != nil {
		pbmsg, ok := res.(proto.Message)
		if !ok {
			return nil, gerror.Wrapf(err, "res is not proto.Message, roleID: %d", r.RoleID)
		}
		svrMsg, err := r.newServerMsg(pbmsg)
		if err != nil {
			return nil, gerror.Wrapf(err, "send server msg error, roleID: %d", r.RoleID)
		}
		rsp = svrMsg
	}
	return rsp, nil
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
	// 把各个模块的handle也添加到msgHandler中,方便自动处理协议
	for _, mod := range r.Modules() {
		r.AddMsgHandler(mod)
	}
}

func (r *RoleMain) TickSave(ctx context.Context, _info gxytimer.TimerActiveInfo) {
	if err := r.save(ctx); err != nil {
		glog.Errorf(ctx, "save error, roleID: %d, err: %+v", r.RoleID, err)
		// 终止当前进程
		r.Stop(err)
		return
	}
}

func (r *RoleMain) DayRefresh(ctx context.Context, info gxytimer.TimerActiveInfo) {
}

func (r *RoleMain) save(_ context.Context) error {
	var errStr string
	for _, mod := range r.Modules() {
		rmod, _ := mod.(IRoleModule)
		if rmod == nil {
			continue
		}
		modState := rmod.PersistState()
		if modState == nil || !modState.IsDirty() {
			continue
		}
		modState.SetUpdateAt(time.Now())

		if err := gxypgx.DB().Save(modState).Error; err != nil {
			tableName := modState.(tabler).TableName()
			errStr += fmt.Sprintf("save mod %s failed: %s", tableName, err)
			continue
		}
		modState.ClearDirty()
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
	newSession := r.Sender()
	if r.state == RoleStateLogined && !gxyactor.PidEqual(r.session, newSession) { // 表示重复登录
		// 断开旧连接
		r.Send(r.session, &pb.ActorStop{
			Reason: "multi login",
		})
	}
	r.session = newSession
	firstLogin := false
	now := time.Now()
	if r.Basic.CreateTm.IsZero() {
		if err := r.OnRoleCreated(ctx); err != nil {
			return nil, err
		}
		firstLogin = true
		r.Basic.CreateTm = now
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
		RoleId:     r.RoleID,
	}, nil
}

func (r *RoleMain) OnRoleCreated(ctx context.Context) error {
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		rmod.OnCreate(ctx)
	}
	r.Public.UpdateRolePublic(ctx)
	// 发放初始物品
	initItems := gameconfig.GameConfig().TbGlobalConfig.Get().InitItems
	if len(initItems) > 0 {
		if err := r.Bag.AddItemStack(ctx, initItems); err != nil {
			return err
		}
	}
	// 建号强制保存一次
	if err := r.save(ctx); err != nil {
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
	if err := r.save(ctx); err != nil {
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
	if serr := r.save(ctx); serr != nil {
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
