package logic

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxynet/codec"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"
	"gserver/core/gxytimer"
	"gserver/core/gxyutil"
	"gserver/protocol/pb"
	"gserver/src/apps/role"
	"gserver/src/lib"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/errors/gerror"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

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
	AccountUid       string       // 账号ID
	RoleID           int64        // 玩家ID（认证后才有）
	ConnectTime      time.Time    // 连接时间
	ClientLastActive time.Time    // 客户端最后活跃时间
	ServerLastActive time.Time    // 服务器最后活跃时间
	RolePid          gxyactor.PID // 角色PID
}

// Session 会话Actor，继承自ActorBase
type Session struct {
	*gxyactor.ActorBase
	endpoint    endpoint.Endpoint // 网络端点
	state       SessionState      // 会话状态
	sessionInfo *SessionInfo      // 会话信息
}

func NewSession(ep endpoint.Endpoint) *Session {
	s := &Session{
		endpoint:    ep,
		sessionInfo: &SessionInfo{},
	}
	ctx := gxylog.NewContext(context.Background(), "session")
	s.ActorBase = gxyactor.NewActorBase(ctx, s, "session")
	return s
}

func (s *Session) HandleMessage(ctx context.Context, msg any) error {
	switch msg := msg.(type) {
	case *message.Message:
		gxylog.Debug(ctx, "handle client msg", gxylog.Str("msg", gxyutil.FormatObject(msg)))
		if err := s.OnHandleClientMessage(ctx, msg); err != nil {
			return gerror.Wrap(err, "handle client message error")
		}
	case *pb.ServerMsg:
		if err := s.OnHandleServerMessage(ctx, msg); err != nil {
			return gerror.Wrap(err, "handle server message error")
		}
	case *actor.Terminated:
		if gxyactor.PidEqual(msg.Who, s.sessionInfo.RolePid) {
			s.sessionInfo.RolePid = nil
			s.Stop(gerror.New("role terminated"))
		}
	case *pb.ActorError:
		s.Stop(gerror.New(msg.Reason))
	}
	return nil
}

// NewSession 创建会话
// OnModInit Actor初始化
func (s *Session) Init(ctx context.Context, args []any) error {
	s.sessionInfo = &SessionInfo{
		ConnectTime:      time.Now(),
		ClientLastActive: time.Now(),
	}
	gxylog.Info(ctx, "Session initialized", gxylog.Str("remote", s.endpoint.Conn().RemoteAddr().String()))
	s.state = StateConnected

	return nil
}

func (s *Session) DelayInit(ctx context.Context) error {
	s.Timer().AddTick(ctx, &gxytimer.Tick{
		Name:     "check",
		Interval: SESSION_CHECK_INTERVAL,
	}, s.sessionCheck)
	s.updateClientLastActive()
	s.updateServerLastActive()
	return nil
}

func (s *Session) sessionCheck(ctx context.Context, _ gxytimer.TimerActiveInfo) {
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
	s.Span().SetName(fmt.Sprintf("%T", firstpacket))

	s.sessionInfo.AccountUid = firstpacket.AccountUid
	roleID, err := role.GetRoleIDByAccount(ctx, firstpacket.AccountUid)
	if err != nil {
		return gerror.Wrapf(err, "get role id error, account: %s", firstpacket.AccountUid)
	}
	s.SetLogValue(gxylog.ContextKeyRoleID, roleID)
	rolePid, err := lib.ActivateRole(ctx, roleID)
	if err != nil {
		return gerror.Wrapf(err, "activate role actor error, role: %d", roleID)
	}
	gxylog.Info(ctx, "get role pid", gxylog.Any("rolePid", rolePid))
	s.sessionInfo.RolePid = rolePid
	s.sessionInfo.RoleID = roleID

	s.Actx.Watch(rolePid)
	rsp := &pb.RspHandShake{
		AccountUid: firstpacket.AccountUid,
		RoleId:     roleID,
	}
	if err := s.endpoint.SendMsg(rsp); err != nil {
		return gerror.Newf("send rsp error, err: %v", err)
	}
	SessionMgr().Add(roleID, s.Self())
	s.state = StateHandshake
	s.Span().SetAttributes(
		attribute.String("accountUid", firstpacket.AccountUid),
		attribute.Int64("roleID", roleID),
	)
	return nil
}

// OnHandleMessage 处理异步消息
func (s *Session) OnHandleClientMessage(ctx context.Context, msg *message.Message) error {
	s.updateClientLastActive()
	switch msg.Type {
	case message.MESSGE_TYPE_FIRST_PACKET:
		if err := s.handleHandshake(ctx, msg.Msg); err != nil {
			return gerror.Wrap(err, "handle handshake error")
		}
	case message.MESSAGE_TYPE_DATA_PACKET:
		// 转发消息给角色actor
		pbmsg, ok := msg.Msg.(proto.Message)
		if !ok {
			return gerror.Newf("msg is not pb.RemoteReqMsg, msg: %s", gxyutil.FormatObject(pbmsg))
		}
		s.Span().SetName(fmt.Sprintf("%T", pbmsg))
		s.Span().SetAttributes(
			attribute.Int64("roleID", s.sessionInfo.RoleID),
		)
		switch pbmsg.(type) {
		case *pb.ReqAccountLogout:
			s.Stop(gerror.New("client account logout"))
		default:
			gxylog.Debug(ctx, "recv client msg", gxylog.Str("path", msg.Path), gxylog.Str("msg", gxyutil.FormatObject(pbmsg)))
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
	gxyactor.CallSync(ctx, s.sessionInfo.RolePid, req, s.Self())
	return nil
}

func (s *Session) OnHandleServerMessage(ctx context.Context, msg *pb.ServerMsg) error {
	s.updateServerLastActive()
	// 解析响应消息
	pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
	if err != nil {
		return gerror.Wrap(err, "unmarshal rsp error, err: %v")
	}

	s.Span().SetName(fmt.Sprintf("%T", pbmsg))
	s.Span().SetAttributes(
		attribute.Int64("roleID", s.sessionInfo.RoleID),
	)
	switch pbmsg.(type) {
	case *pb.RspAccountLogin:
		s.state = StateLogin
	}
	gxylog.Debug(ctx, "send client msg", gxylog.Str("msg", gxyutil.FormatObject(pbmsg)))
	if err := s.endpoint.SendMsg(pbmsg); err != nil {
		return gerror.Wrap(err, "send rsp error, err: %v")
	}

	return nil
}

// Terminate 终止会话
func (s *Session) Terminate(ctx context.Context, err error) {
	gxylog.Info(ctx, "session terminating", gxylog.Num("roleID", s.sessionInfo.RoleID), gxylog.Err(err))
	SessionMgr().Remove(s.sessionInfo.RoleID)
	// 关闭网络连接
	if s.endpoint != nil {
		s.endpoint.SetData(nil)
		s.endpoint.Conn().Close()
	}
	if s.sessionInfo.RolePid != nil {
		s.Actx.Unwatch(s.sessionInfo.RolePid)
		msg := &pb.ReqAccountLogout{
			Reason: fmt.Sprintf("session terminated: %s", err.Error()),
		}
		s.SendRoleMsg(ctx, msg, codec.MessageMetaByMsg(msg).ID)
	}
	s.state = StateDisconnected

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
