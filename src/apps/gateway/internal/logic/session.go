package logic

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxynet/codec"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"
	"gserver/core/gxyutil"
	"gserver/protocol/pb"
	"gserver/src/lib"
	"gserver/src/lib/gatetoken"

	"github.com/gogf/gf/v2/errors/gerror"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const gateMaintenanceEnv = "GATE_MAINTENANCE"

var gateMaintenanceEnabled = func() bool {
	return os.Getenv(gateMaintenanceEnv) == "true" || os.Getenv(gateMaintenanceEnv) == "1"
}

var verifyGateToken = func(token string) (*gatetoken.Claims, error) {
	return nil, gerror.New("gate token verifier not configured")
}

func SetGateTokenVerifier(verifier func(token string) (*gatetoken.Claims, error)) {
	verifyGateToken = verifier
}

var activateRole = func(ctx context.Context, roleID int64) (gxyactor.PID, error) {
	return lib.ActivateRole(ctx, roleID)
}

// watchRole/unwatchRole 可替换函数变量:测试注入以观察监视行为(编译期安全,非 gomonkey)。
// 会话必须监视角色进程——角色终止时要据此断开连接。
var (
	watchRole = func(s *Session, pid gxyactor.PID) error {
		return s.Watch(pid)
	}
	unwatchRole = func(s *Session, pid gxyactor.PID) error {
		return s.Unwatch(pid)
	}
)

// 错误契约: 准入错误(限流 sentinel / 未配置 / ctx 取消)原样上抛, 不包裹——
// 预期拒绝需要保持原始形态供 OnHandleClientMessage 分类; ActivateRole 的业务
// 错误在此打上激活上下文后返回。
func activateRoleWithLoginPermit(ctx context.Context, roleID int64) (gxyactor.PID, error) {
	permit, err := currentLoginAcquirer.acquire(ctx)
	if err != nil {
		return gxyactor.PID{}, err
	}
	defer permit.Release()
	pid, err := activateRole(ctx, roleID)
	if err != nil {
		return gxyactor.PID{}, gerror.Wrapf(err, "activate role actor error, role: %d", roleID)
	}
	return pid, nil
}

// isLoginAdmissionRejection 判定是否为预期的登录准入拒绝（限流/队列满/队列超时）。
// 注意：三个准入 sentinel 必须以未被 gerror 包裹的原始形态到达本函数——
// cockroachdb errors.Is 无法穿透 gerror.Wrap 匹配 sentinel（gerror.Cause 会过度
// 解包 pkg/errors 风格的 causer）。当前生产路径（activateRoleWithLoginPermit 原样
// 上抛准入错误，handleHandshake 直接透传，OnHandleClientMessage 在其 Wrap 之前
// 分类）恰好满足该不变式。
func isLoginAdmissionRejection(err error) bool {
	return errors.Is(err, ErrLoginRateLimited) ||
		errors.Is(err, ErrLoginQueueFull) ||
		errors.Is(err, ErrLoginQueueTimeout)
}

const (
	SESSION_MSG_STOP            = "stop"
	SESSION_MSG_CLIENT          = "client"         // 客户端消息
	SESSION_CLIENT_IDLE_TIMEOUT = 10 * time.Minute // 空闲超时时间
	SESSION_SERVER_IDLE_TIMEOUT = 10 * time.Minute // 空闲超时时间
	SESSION_CHECK_INTERVAL      = 30 * time.Second
)

// SessionState 会话状态
type SessionState int

const (
	StateConnected SessionState = iota // 已连接
	StateHandshake                     // 已登录
	StateLogin
	StateDisconnected // 已断开
)

// SessionInfo 会话信息
type SessionInfo struct {
	AccountID        string       // 账号ID
	RoleID           int64        // 玩家ID（认证后才有）
	ConnectTime      time.Time    // 连接时间
	ClientLastActive time.Time    // 客户端最后活跃时间
	ServerLastActive time.Time    // 服务器最后活跃时间
	RolePid          gxyactor.PID // 角色PID
}

// Session 会话Actor，继承自ActorBase
type Session struct {
	*gxyactor.Actor
	endpoint    endpoint.Endpoint // 网络端点
	state       SessionState      // 会话状态
	sessionInfo *SessionInfo      // 会话信息
}

func NewSession(ep endpoint.Endpoint) *Session {
	s := &Session{
		endpoint:    ep,
		sessionInfo: &SessionInfo{},
	}
	s.Actor = gxyactor.NewActor()
	return s
}

// HandleMessage 是业务入口(异步)。消息已由门面还原,此处直接分流。
// 会话的来源固定(客户端、角色、监视通知),不走反射分派。
func (s *Session) HandleMessage(msg any) (any, error) {
	ctx := s.Ctx
	switch msg := msg.(type) {
	case *message.Message:
		gxylog.Debug(ctx, "handle client msg", gxylog.Str("payload", gxyutil.FormatObject(msg)))
		if err := s.OnHandleClientMessage(ctx, msg, s.Sender()); err != nil {
			return nil, gerror.Wrap(err, "handle client message error")
		}
	case *pb.ServerMsg:
		if err := s.OnHandleServerMessage(ctx, msg); err != nil {
			return nil, gerror.Wrap(err, "handle server message error")
		}
	case *pb.ActorError:
		s.Stop(gerror.New(msg.Reason))
	}
	return nil, nil
}

// Init 是同步初始化段:会话建立时初始化状态并启动空闲检查。
func (s *Session) Init(args ...any) error {
	ctx := s.Ctx
	s.sessionInfo = &SessionInfo{
		ConnectTime:      time.Now(),
		ClientLastActive: time.Now(),
	}
	gxylog.Info(ctx, "Session initialized", gxylog.Str("remote", s.endpoint.Conn().RemoteAddr().String()))
	s.state = StateConnected

	s.Timer().AddTick("check", SESSION_CHECK_INTERVAL, s.sessionCheck)
	s.updateClientLastActive()
	s.updateServerLastActive()
	return nil
}

func (s *Session) sessionCheck(ctx context.Context) {
	clientIdleTime := time.Since(s.sessionInfo.ClientLastActive)
	if clientIdleTime > SESSION_CLIENT_IDLE_TIMEOUT {
		s.Stop(errors.New("client idle timeout"))
		return
	}
	serverIdleTime := time.Since(s.sessionInfo.ServerLastActive)
	// 客户端发了包，但是服务器超过时间没有响应
	if serverIdleTime > SESSION_SERVER_IDLE_TIMEOUT {
		s.Stop(errors.New("server idle timeout"))
		return
	}
}

func (s *Session) handleHandshake(ctx context.Context, msg any) error {
	firstpacket, ok := msg.(*pb.ReqHandShake)
	if !ok {
		return gerror.Newf("first packet is not pb.ReqHandShake, msg: %v", msg)
	}
	if gateMaintenanceEnabled() {
		return gerror.New("gate maintenance")
	}

	identity, err := resolveHandshakeIdentity(firstpacket.GetGateToken())
	if err != nil {
		return gerror.Wrap(err, "resolve handshake identity failed")
	}
	s.sessionInfo.AccountID = identity.AccountID
	s.sessionInfo.RoleID = identity.RoleID

	s.SetLogValue(gxylog.CONTEXT_KEY_ROLE_ID, identity.RoleID)
	rolePid, err := activateRoleWithLoginPermit(ctx, identity.RoleID)
	if err != nil {
		return err
	}
	gxylog.Info(ctx, "get role pid", gxylog.Any("rolePid", rolePid))
	s.sessionInfo.RolePid = rolePid

	if err := watchRole(s, rolePid); err != nil {
		gxylog.Warn(ctx, "watch role failed", gxylog.Num("roleID", identity.RoleID), gxylog.Err(err))
	}
	rsp := &pb.RspHandShake{
		AccountUid: identity.AccountID,
		RoleId:     identity.RoleID,
	}
	if err := s.sendClientMsg(ctx, rsp); err != nil {
		return err
	}
	SessionMgr().Add(identity.RoleID, s.Self())
	gxymetrics.OnlinePlayers.Set(float64(SessionMgr().Count()))
	s.state = StateHandshake
	s.SetTracingSpanAttribute("accountUid", identity.AccountID)
	s.SetTracingSpanAttribute("roleID", strconv.FormatInt(identity.RoleID, 10))
	return nil
}

type handshakeIdentity struct {
	AccountID string
	RoleID    int64
}

func resolveHandshakeIdentity(token string) (*handshakeIdentity, error) {
	if token == "" {
		return nil, gerror.New("gate token required")
	}
	claims, err := verifyGateToken(token)
	if err != nil {
		return nil, gerror.Wrap(err, "verify gate token failed")
	}
	return &handshakeIdentity{
		AccountID: claims.AccountID,
		RoleID:    claims.RoleID,
	}, nil
}

// OnHandleMessage 处理异步消息
// HandleDown 是监视通知入口:被监视的角色进程终止。
// 注意:该通知早于对端终止回调完成,因此这里只做会话清理,
// 不读取对端状态、不据此释放所有权。
func (s *Session) HandleDown(pid gxyactor.PID) {
	if gxyactor.PidEqual(pid, s.sessionInfo.RolePid) {
		s.sessionInfo.RolePid = gxyactor.PID{}
		s.Stop(errors.New("role terminated"))
	}
}

func (s *Session) OnHandleClientMessage(ctx context.Context, msg *message.Message, from gxyactor.PID) error {
	s.updateClientLastActive()
	switch msg.Type {
	case message.MESSGE_TYPE_FIRST_PACKET:
		if err := s.handleHandshake(ctx, msg.Msg); err != nil {
			if isLoginAdmissionRejection(err) {
				s.Stop(err)
				return nil
			}
			return gerror.Wrap(err, "handle handshake error")
		}
	case message.MESSAGE_TYPE_DATA_PACKET:
		// 转发消息给角色actor
		pbmsg, ok := msg.Msg.(proto.Message)
		if !ok {
			return gerror.Newf("msg is not pb.RemoteReqMsg, msg: %s", gxyutil.FormatObject(pbmsg))
		}
		s.SetTracingSpanAttribute("roleID", strconv.FormatInt(s.sessionInfo.RoleID, 10))
		switch pbmsg.(type) {
		case *pb.ReqAccountLogout:
			s.Stop(gerror.New("client account logout"))
		default:
			gxylog.Debug(ctx, "recv client msg", gxylog.Str("path", msg.Path), gxylog.Str("payload", gxyutil.FormatObject(pbmsg)))
			if err := s.SendRoleMsg(ctx, pbmsg, msg.Path); err != nil {
				return gerror.Wrap(err, "send data msg error")
			}
		}

	}

	return nil
}

func (s *Session) SendRoleMsg(ctx context.Context, msg proto.Message, id string) error {
	req := &pb.ClientMsg{
		Id:  id,
		Msg: &anypb.Any{},
	}
	if err := anypb.MarshalFrom(req.Msg, msg, proto.MarshalOptions{}); err != nil {
		return gerror.Newf("marshal req error, err: %v", err)
	}
	// 投递失败不使客户端消息处理失败:对端的回复会回到本进程邮箱。
	// 与既有语义一致(发送错误只记日志),不在此处改变会话状态。
	if err := s.SendTo(s.sessionInfo.RolePid, req); err != nil {
		gxylog.Warn(ctx, "send role msg failed",
			gxylog.Num("roleID", s.sessionInfo.RoleID), gxylog.Err(err))
	}
	return nil
}

func (s *Session) OnHandleServerMessage(ctx context.Context, msg *pb.ServerMsg) error {
	s.updateServerLastActive()
	// 解析响应消息
	pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
	if err != nil {
		return gerror.Wrap(err, "unmarshal rsp error, err: %v")
	}

	s.SetTracingSpanAttribute("roleID", strconv.FormatInt(s.sessionInfo.RoleID, 10))
	switch pbmsg.(type) {
	case *pb.RspAccountLogin:
		s.state = StateLogin
	}
	if s.state == StateDisconnected {
		gxylog.Debug(ctx, "session disconnected, skip send", gxylog.Str("payload", gxyutil.FormatObject(pbmsg)))
		return nil
	}
	gxylog.Debug(ctx, "send client msg", gxylog.Str("payload", gxyutil.FormatObject(pbmsg)))
	if err := s.sendClientMsg(ctx, pbmsg); err != nil {
		return err
	}

	return nil
}

func (s *Session) sendClientMsg(ctx context.Context, msg proto.Message) error {
	if err := s.endpoint.SendMsg(msg); err != nil {
		gxylog.Debug(ctx, "send client msg failed, stop session", gxylog.Err(err))
		s.Stop(errors.Wrap(err, "conn closed: send client msg failed"))
		return nil
	}
	return nil
}

// Terminate 终止会话
// Terminate 是运行时回调:清理会话状态、关闭连接,最后由基类停定时器。
func (s *Session) Terminate(err error) {
	ctx := s.Ctx
	gxylog.Debug(ctx, "session terminating", gxylog.Num("roleID", s.sessionInfo.RoleID), gxylog.Err(err))
	SessionMgr().Remove(s.sessionInfo.RoleID)
	gxymetrics.OnlinePlayers.Set(float64(SessionMgr().Count()))
	gxymetrics.SessionDisconnects.WithLabelValues(sessionDisconnectReason(err)).Inc()
	// 关闭网络连接
	if s.endpoint != nil {
		s.endpoint.SetData(nil)
		s.endpoint.Close()
	}
	if !gxyactor.PIDIsZero(s.sessionInfo.RolePid) {
		if err := unwatchRole(s, s.sessionInfo.RolePid); err != nil {
			gxylog.Warn(ctx, "unwatch role failed", gxylog.Err(err))
		}
		msg := &pb.ReqAccountLogout{
			Reason: fmt.Sprintf("session terminated: %s", err.Error()),
		}
		_ = s.SendRoleMsg(ctx, msg, codec.MessageMetaByMsg(msg).ID)
	}
	s.state = StateDisconnected
}

func sessionDisconnectReason(err error) string {
	if err == nil {
		return "unknown"
	}
	// 与 isLoginAdmissionRejection 相同的不变式：sentinel 需以未包裹形态到达这里。
	// 当前生产路径（OnHandleClientMessage 将原始 sentinel 传给 s.Stop）满足该不变式。
	switch {
	case errors.Is(err, ErrLoginRateLimited):
		return "login_rate_limited"
	case errors.Is(err, ErrLoginQueueFull):
		return "login_queue_full"
	case errors.Is(err, ErrLoginQueueTimeout):
		return "login_queue_timeout"
	}
	reason := strings.ToLower(err.Error())
	switch {
	case strings.Contains(reason, "client account logout"):
		return "client_logout"
	case strings.Contains(reason, "client idle timeout"):
		return "client_idle_timeout"
	case strings.Contains(reason, "server idle timeout"):
		return "server_idle_timeout"
	case strings.Contains(reason, "role terminated"):
		return "role_terminated"
	case strings.Contains(reason, "multi login"):
		return "multi_login"
	case strings.Contains(reason, "gateway service stop"):
		return "service_stop"
	case strings.Contains(reason, "conn closed"):
		return "conn_closed"
	default:
		return "error"
	}
}

// updateClientLastActive 更新最后活跃时间
func (s *Session) updateClientLastActive() {
	s.sessionInfo.ClientLastActive = time.Now()
}

// updateServerLastActive 更新服务器最后活跃时间
func (s *Session) updateServerLastActive() {
	s.sessionInfo.ServerLastActive = time.Now()
}

// GetSessionInfo 获取会话信息
func (s *Session) GetSessionInfo() *SessionInfo {
	return s.sessionInfo
}
