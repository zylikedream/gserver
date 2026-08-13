package logic

import (
	"context"
	"errors"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxymodule"
	"gserver/core/gxynet/codec"
	"gserver/core/gxypgx"
	"gserver/core/gxyredis"
	"gserver/core/gxytimer"
	"gserver/core/gxyutil"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/role/internal/logic/bag"
	"gserver/src/lib/rolelib"
	"gserver/src/pkg/deps"
	"gserver/src/pkg/gameconfig"
	"reflect"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/util/gconv"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	SESSION_ALIVE_INTERVAL = 10 * time.Minute
	PERSIST_INTERVAL       = 600 * time.Second
	SINGLE_ALIVE_INTERVAL  = 10 * time.Minute
	PUBLIC_UPDATE_INTERVAL = 8 * time.Minute
	SLOW_CLIENT_REQUEST    = 200 * time.Millisecond
	ROLE_SAVE_CONCURRENCY  = 16
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
	globalRoleSaveLimiter = newRoleSaveLimiter(ROLE_SAVE_CONCURRENCY)
)

type RoleState int32

const (
	RoleStateInit    RoleState = iota
	RoleStateLoad              // 角色已初始化，等待登录
	RoleStateLogined           // 角色已登录, 开始正常处理消息
	RoleStateLogout            // 角色已登出
)

type roleModules struct {
	Bag           *RoleBag
	Basic         *RoleBasic
	Public        *RolePublic
	Extra         *RoleExtra
	Flower        *RoleFlower
	Plot          *RolePlot
	Steal         *RoleSteal
	MainTask      *RoleMainTask
	ResidentOrder *RoleResidentOrder
	GM            *RoleGM
	Chat          *RoleChat
	Guild         *RoleGuild
	Friend        *RoleFriend
	Mail          *RoleMail
}

type RoleMain struct {
	gxymodule.ModuleBase
	roleModules
	*gxyactor.ActorBase
	RoleID int64

	deps deps.Deps

	modsHash          map[string]uint64
	session           gxyactor.PID
	state             RoleState
	sessionActiveTime time.Time
	eventBus          event.IEventBus
}

func NewRoleMain() *RoleMain {
	r := &RoleMain{
		state: RoleStateInit,
		// 组装根:填充全局单例;测试可覆盖注入 mock。
		deps: deps.Deps{DB: gxypgx.DB(), Redis: gxyredis.Redis(), Cfg: gameconfig.Get()},
	}
	ctx := gxylog.NewContext(context.Background(), "role")
	r.ActorBase = gxyactor.NewActorBase(ctx, r, "role")
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
	accountID, err := lookupAccountIDByRoleID(ctx, r.RoleID)
	if err != nil {
		return err
	}
	if accountID == "" {
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
	gxylog.Debug(ctx, "role start success")
	return nil
}

func (r *RoleMain) initRole(ctx context.Context) error {
	if err := r.initModules(ctx); err != nil {
		return err
	}
	r.initTimer(ctx)
	r.initMsgHandler()
	r.state = RoleStateLoad
	gxylog.Debug(ctx, "init role success")
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
	r.SetSelfMod(r)
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
		if err := loadModuleState(ctx, r.deps.DB, roleID, modState); err != nil {
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

// DB 返回数据库连接,语义同 RoleModule.DB(测试注入 mock 后走 mock)。
func (r *RoleMain) DB() *gorm.DB {
	if r.deps.DB != nil {
		return r.deps.DB
	}
	return gxypgx.DB()
}

// Redis 返回缓存客户端,语义同 DB。
func (r *RoleMain) Redis() gxyredis.Client {
	if r.deps.Redis != nil {
		return r.deps.Redis
	}
	return gxyredis.Redis()
}

// Cfg 返回游戏配表,语义同 DB。
func (r *RoleMain) Cfg() *gameconfig.GameConfig {
	if r.deps.Cfg != nil {
		return r.deps.Cfg
	}
	return gameconfig.Get()
}

func loadModuleState(_ context.Context, db *gorm.DB, roleID int64, modState IPersistState) error {
	tableName := modState.(tabler).TableName()
	err := db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).
		Table(tableName).Where("role_id = ?", roleID).First(modState).Error
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
	_, err := r.AutoHandleMsg(ctx, msg)
	return err
}

func (r *RoleMain) HandleClientMsg(ctx context.Context, climsg *pb.ClientMsg) (proto.Message, error) {
	id := climsg.Id
	start := time.Now()
	msgID := id
	msgName := "unknown"
	result := "error"
	defer func() {
		gxymetrics.ClientRequests.WithLabelValues(msgID, msgName, result).Inc()
		cost := time.Since(start)
		gxymetrics.ObserveWithTrace(ctx, gxymetrics.ClientRequestDuration.WithLabelValues(msgID, msgName, result), cost.Seconds())
		if cost >= SLOW_CLIENT_REQUEST {
			gxylog.Warn(ctx, "slow client request",
				gxylog.Str("msg_id", msgID),
				gxylog.Str("msg_name", msgName),
				gxylog.Str("result", result),
				gxylog.Num("cost_ms", cost.Milliseconds()),
			)
		}
	}()

	pbmsg, err := anypb.UnmarshalNew(climsg.GetMsg(), proto.UnmarshalOptions{})
	if err != nil {
		return nil, gerror.Wrapf(err, "unmarshal req error, roleID: %d", r.RoleID)
	}
	msgID, msgName = clientMessageMetricLabels(id, pbmsg)
	r.Span().SetName(fmt.Sprintf("%T", pbmsg))
	r.Span().SetAttributes(
		attribute.Int64("roleID", r.RoleID),
	)
	r.sessionActiveTime = time.Now()
	gxylog.Debug(ctx, "role recv client msg",
		gxylog.Str("msg_id", msgID),
		gxylog.Str("msg_name", msgName),
		gxylog.Num("role_id", r.RoleID),
	)
	if !canHandleMsg(r.state, pbmsg) {
		gxylog.Warn(ctx, "role recv msg in wrong state, ignore", gxylog.Num("state", int(r.state)), gxylog.Str("payload", gxyutil.FormatObject(pbmsg)))
		result = "ignored"
		return nil, nil
	}
	var rsp proto.Message
	res, err := r.DoCallMsgHandler(ctx, pbmsg)
	if err != nil {
		result = "error"
		res = &pb.Ack{
			Code:   1,
			Id:     id,
			Reason: err.Error(),
		}
	} else {
		result = "ok"
	}
	if res != nil {
		pbmsg, ok := res.(proto.Message)
		if !ok {
			result = "error"
			return nil, gerror.Wrapf(err, "res is not proto.Message, roleID: %d", r.RoleID)
		}
		svrMsg, err := r.newServerMsg(pbmsg)
		if err != nil {
			result = "error"
			return nil, gerror.Wrapf(err, "send server msg error, roleID: %d", r.RoleID)
		}
		rsp = svrMsg
	}
	return rsp, nil
}

func clientMessageMetricLabels(id string, msg proto.Message) (string, string) {
	meta := codec.MessageMetaByMsg(msg)
	if meta == nil {
		return id, string(msg.ProtoReflect().Descriptor().Name())
	}
	if meta.ID != "" {
		id = meta.ID
	}
	return id, meta.Name
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
		gxylog.Error(ctx, "save error", gxylog.Num("roleID", r.RoleID), gxylog.Err(err))
		// 终止当前进程
		r.Stop(err)
		return
	}
}

func (r *RoleMain) DayRefresh(ctx context.Context, info gxytimer.TimerActiveInfo) {
}

func (r *RoleMain) SaveRoleModule(ctx context.Context, rmod IRoleModule) error {
	modState := rmod.PersistState()
	if modState == nil {
		return nil
	}

	if !modState.IsDirty() {
		return nil
	}
	tableName := modState.(tabler).TableName()
	gxylog.Debug(ctx, "save mod", gxylog.Str("table", tableName))

	// 第一层：Redis 归属检查
	if !r.checkRoleSaveOwner(ctx) {
		return nil
	}

	release, err := globalRoleSaveLimiter.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	saved, err := r.saveRoleModuleState(ctx, r.DB(), rmod)
	if err != nil {
		return err
	}
	if saved != nil {
		saved.state.ClearDirty()
	}
	return nil
}

func (r *RoleMain) checkRoleSaveOwner(ctx context.Context) bool {
	key := rolelib.GetRoleLocateKey(r.RoleID)
	owner, err := r.Redis().Get(ctx, key).Result()
	if err == redis.Nil || owner == "" {
		gxylog.Warn(ctx, "actor not claimed in redis, skip save", gxylog.Num("roleID", r.RoleID))
		return false
	}
	if err != nil {
		gxylog.Error(ctx, "redis get failed, skip save", gxylog.Num("roleID", r.RoleID), gxylog.Err(err))
		return false
	}
	if owner != gxyactor.ActorApp().NodeInstanceName() {
		gxylog.Warn(ctx, "actor claimed by another node, skip save", gxylog.Num("roleID", r.RoleID), gxylog.Str("owner", owner))
		return false
	}
	return true
}

type savedRoleModule struct {
	state          IPersistState
	oldVersion     int64
	versionChanged bool
}

type roleSaveLimiter struct {
	slots chan struct{}
}

func newRoleSaveLimiter(limit int) *roleSaveLimiter {
	if limit <= 0 {
		limit = 1
	}
	return &roleSaveLimiter{
		slots: make(chan struct{}, limit),
	}
}

func (l *roleSaveLimiter) acquire(ctx context.Context) (func(), error) {
	select {
	case l.slots <- struct{}{}:
		return func() { <-l.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *RoleMain) saveRoleModuleState(ctx context.Context, db *gorm.DB, rmod IRoleModule) (*savedRoleModule, error) {
	modState := rmod.PersistState()
	if modState == nil {
		return nil, nil
	}
	if !modState.IsDirty() {
		return nil, nil
	}
	tableName := modState.(tabler).TableName()

	oldVersion := modState.GetVersion()
	modState.SetUpdateAt(time.Now())

	if oldVersion == 0 {
		// version==0 表示新号，行还不存在，直接 Save（INSERT）
		if err := db.Save(modState).Error; err != nil {
			return nil, fmt.Errorf("save mod %s failed: %s", tableName, err)
		}
		return &savedRoleModule{state: modState}, nil
	}

	// version>0 表示有已有行，UPDATE + WHERE version 做冲突检测
	modState.SetVersion(oldVersion + 1)
	result := db.Model(modState).
		Where("role_id = ? AND version = ?", r.RoleID, oldVersion).
		Updates(modState)
	if result.Error != nil {
		modState.SetVersion(oldVersion)
		return nil, fmt.Errorf("save mod %s failed: %s", tableName, result.Error)
	}
	if result.RowsAffected == 0 {
		modState.SetVersion(oldVersion) // 不清 dirty，下次重试
		return nil, nil
	}
	return &savedRoleModule{state: modState, oldVersion: oldVersion, versionChanged: true}, nil
}

func (r *RoleMain) save(ctx context.Context) error {
	dirtyMods := r.dirtyRoleModules()
	if len(dirtyMods) == 0 {
		return nil
	}

	if !r.checkRoleSaveOwner(ctx) {
		return nil
	}

	release, err := globalRoleSaveLimiter.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()

	var savedMods []*savedRoleModule
	err = r.DB().Transaction(func(tx *gorm.DB) error {
		var errStr string
		for _, rmod := range dirtyMods {
			saved, err := r.saveRoleModuleState(ctx, tx, rmod)
			if err != nil {
				errStr += err.Error()
				continue
			}
			if saved != nil {
				savedMods = append(savedMods, saved)
			}
		}
		if errStr != "" {
			return errors.New(errStr)
		}
		return nil
	})
	if err != nil {
		for _, saved := range savedMods {
			if saved.versionChanged {
				saved.state.SetVersion(saved.oldVersion)
			}
		}
		return err
	}
	for _, saved := range savedMods {
		saved.state.ClearDirty()
	}
	return nil
}

func (r *RoleMain) dirtyRoleModules() []IRoleModule {
	var dirtyMods []IRoleModule
	for _, mod := range r.Modules() {
		rmod, _ := mod.(IRoleModule)
		if rmod == nil || !roleModuleDirty(rmod) {
			continue
		}
		dirtyMods = append(dirtyMods, rmod)
	}
	return dirtyMods
}

func roleModuleDirty(rmod IRoleModule) bool {
	modState := rmod.PersistState()
	return modState != nil && modState.IsDirty()
}

func roleModuleTableName(rmod IRoleModule) string {
	modState := rmod.PersistState()
	if modState == nil {
		return ""
	}
	if t, ok := modState.(tabler); ok {
		return t.TableName()
	}
	return ""
}

func (r *RoleMain) SendClient(ctx context.Context, msg proto.Message) {
	if r.session == nil {
		return
	}
	svrMsg, err := r.newServerMsg(msg)
	if err != nil {
		gxylog.Error(ctx, "new server msg error", gxylog.Num("roleID", r.RoleID), gxylog.Err(err))
		return
	}
	gxyactor.Send(ctx, r.session, svrMsg)
}

func (r *RoleMain) PublishRoleEvent(ctx context.Context, eventType event.EventType, data any) {
	if r.eventBus == nil {
		return
	}
	r.eventBus.Publish(ctx, eventType, data)
}

func (r *RoleMain) SubscribeRoleEvent(eventType event.EventType, handler func(ctx context.Context, event event.EventParam)) event.EventRef {
	if r.eventBus == nil {
		return ""
	}
	return r.eventBus.Subscribe(eventType, handler)
}

func (r *RoleMain) ReqAccountLogin(ctx context.Context, req *pb.ReqAccountLogin) (rsp *pb.RspAccountLogin, err error) {
	result := "ok"
	defer func() {
		if err != nil {
			result = "error"
		}
		gxymetrics.RoleLogins.WithLabelValues(result).Inc()
	}()
	newSession := r.Sender()
	if r.state == RoleStateLogined && !gxyactor.PidEqual(r.session, newSession) { // 表示重复登录
		// 断开旧连接
		gxyactor.Send(ctx, r.session, &pb.ActorStop{
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
	}
	r.Basic.LoginTm = now
	if r.Basic.LoginTm.Sub(r.Basic.LogoutTm).Seconds() < 2*time.Second.Seconds() {
		gxylog.Info(ctx, "role reconnect", gxylog.Num("roleID", r.RoleID))
	}
	r.Timer().Cancel(ctx, SignleAliveOnce.Name)
	r.state = RoleStateLogined
	r.Public.IsOnline = true
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
		mod.(IRoleModule).OnCreate(ctx)
	}

	r.Public.UpdateRolePublic(ctx)

	// 发放初始物品
	initItems := r.Cfg().TbGlobalConfig.Get().InitItems
	if len(initItems) > 0 {
		if err := r.Bag.SaveGoods(ctx, nil, initItems, "", bag.OptSilent()); err != nil {
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
	r.Timer().AddTick(ctx, PublicUpdateTick, func(ctx context.Context, _info gxytimer.TimerActiveInfo) {
		r.Public.UpdateRolePublic(ctx)
	})
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		start := time.Now()
		rmod.AfterLogin(ctx)
		if cost := time.Since(start); cost >= SLOW_CLIENT_REQUEST {
			gxylog.Warn(ctx, "slow role module after login",
				gxylog.Str("module", fmt.Sprintf("%T", rmod)),
				gxylog.Num("roleID", r.RoleID),
				gxylog.Num("cost_ms", cost.Milliseconds()),
			)
		}
	}
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
	gxymetrics.RoleLogouts.WithLabelValues(roleLogoutReason(reason)).Inc()
	r.Timer().Cancel(ctx, SessionAliveCheckTick.Name)
	r.session = nil
	r.Basic.LogoutTm = time.Now()
	r.Public.IsOnline = false
	r.Public.UpdateRolePublic(ctx)
	if err := r.save(ctx); err != nil {
		return err
	}
	r.Timer().AddOnce(ctx, SignleAliveOnce, func(ctx context.Context, _info gxytimer.TimerActiveInfo) {
		r.Stop(errors.New("single alive timeout"))
	})
	r.state = RoleStateLogout
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		rmod.BeforeLogout(ctx)
	}
	gxylog.Debug(ctx, "role logout", gxylog.Str("reason", reason))
	return nil
}

func roleLogoutReason(reason string) string {
	reason = strings.ToLower(reason)
	switch {
	case strings.Contains(reason, "client account logout"):
		return "client_logout"
	case strings.Contains(reason, "session alive timeout"):
		return "session_alive_timeout"
	case strings.Contains(reason, "session terminated"):
		return "session_terminated"
	default:
		return "unknown"
	}
}

func (r *RoleMain) Terminate(ctx context.Context, err error) {
	gxylog.Debug(ctx, "role stopped", gxylog.Err(err))
	if serr := r.StopModule(ctx); serr != nil {
		gxylog.Error(ctx, "stop module error", gxylog.Err(serr))
	}

	gxylog.Debug(ctx, "role actor terminate", gxylog.Err(err))
}

func (r *RoleMain) OnModStop(ctx context.Context) error {
	gxylog.Debug(ctx, "role stop")
	if serr := r.save(ctx); serr != nil {
		gxylog.Error(ctx, "save error", gxylog.Err(serr))
	}
	return nil
}

func (r *RoleMain) OnNotifyMessage(ctx context.Context, notify *rolelib.OnRoleNotifyMsg) error {
	msg := notify.Msg
	if msg == nil {
		return nil
	}
	switch m := msg.(type) {
	case *pb.NotifyGuildInfo:
		if r.Guild.GuildID == 0 && m.Guild != nil {
			r.Guild.SetGuildID(ctx, m.Guild.Id)
		}
	case *pb.NotifyGuildKicked:
		r.Guild.SetGuildID(ctx, 0)
	case *pb.NotifyMailUpdate:
		if r.Mail != nil {
			if err := r.Mail.RefreshMailCache(ctx); err != nil {
				return err
			}
		}
	}
	r.SendClient(ctx, msg)
	return nil
}
