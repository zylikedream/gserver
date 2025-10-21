package logic

import (
	"context"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"
	"gserver/protocol/pb"
	"gserver/service/role"
	"gserver/util"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	SESSION_MSG_STOP   = "stop"
	SESSION_MSG_CLIENT = "client" // 客户端消息
)

// SessionState 会话状态
type SessionState int

const (
	StateConnected     SessionState = iota // 已连接
	StateAuthenticated                     // 已认证
	StateDisconnected                      // 已断开
)

// SessionInfo 会话信息
type SessionInfo struct {
	AccountUid  string       // 账号ID
	RoleID      int64        // 玩家ID（认证后才有）
	ConnectTime time.Time    // 连接时间
	LastActive  time.Time    // 最后活跃时间
	RolePid     gxyactor.PID // 角色PID
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
		endpoint: ep,
	}
	ctx := gxylog.NewContext(context.Background(), "session")
	s.ActorBase = gxyactor.NewActorBasse(ctx, s)
	return s
}

func (s *Session) HandleMessage(ctx context.Context, msg any) error {
	switch msg := msg.(type) {
	case *message.Message:
		if err := s.OnHandleClientMessage(ctx, msg); err != nil {
			return gerror.Wrap(err, "handle client message error")
		}
	case *pb.ServerMsg:
		if err := s.OnHandleServerMessage(ctx, msg); err != nil {
			return gerror.Wrap(err, "handle server message error")
		}
	case *pb.ActorStop:
		s.Stop(gerror.New(msg.Reason))
	case *actor.Terminated:
		if msg.Who == s.sessionInfo.RolePid {
			s.sessionInfo.RolePid = nil
			s.Stop(gerror.New("role terminated"))
		}
	}
	return nil
}

// NewSession 创建会话
// OnModInit Actor初始化
func (s *Session) Init(ctx context.Context) error {
	s.state = StateConnected
	s.sessionInfo = &SessionInfo{
		ConnectTime: time.Now(),
		LastActive:  time.Now(),
	}
	glog.Infof(ctx, "Session initialized: remote: %s", s.endpoint.Conn().RemoteAddr())

	// 可以在这里进行初始化，如启动心跳等
	return nil
}

func (s *Session) handleHandshake(ctx context.Context, msg any) error {
	firstpacket, ok := msg.(*pb.ReqHandShake)
	if !ok {
		return gerror.Newf("first packet is not pb.ReqHandShake, msg: %v", msg)
	}

	s.sessionInfo.AccountUid = firstpacket.AccountUid
	roleID, err := role.GetRoleIDByAccount(firstpacket.AccountUid)
	if err != nil {
		return gerror.Wrapf(err, "get role id error, account: %s", firstpacket.AccountUid)
	}
	s.SetLogValue(gxylog.ContextKeyRoleID, roleID)
	rolePid, err := role.RoleService().GetRole(roleID)
	if err != nil {
		return gerror.Wrapf(err, "get role id error, role: %d", roleID)
	}
	glog.Infof(ctx, "get role pid: %v", rolePid)
	s.sessionInfo.RolePid = rolePid
	s.sessionInfo.RoleID = roleID

	s.Actx.Watch(rolePid)
	s.state = StateAuthenticated
	rsp := &pb.RspHandShake{
		AccountUid: firstpacket.AccountUid,
		RoleId:     roleID,
	}
	if err := s.endpoint.SendMsg(rsp); err != nil {
		return gerror.Newf("send rsp error, err: %v", err)
	}
	SessionMgr().Add(roleID, s.Self())
	return nil
}

// OnHandleMessage 处理异步消息
func (s *Session) OnHandleClientMessage(ctx context.Context, msg *message.Message) error {
	s.updateLastActive()
	switch msg.Type {
	case message.MESSGE_TYPE_FIRST_PACKET:
		if err := s.handleHandshake(ctx, msg.Msg); err != nil {
			return gerror.Wrap(err, "handle handshake error")
		}
	case message.MESSAGE_TYPE_DATA_PACKET:
		// 转发消息给角色actor
		pbmsg, ok := msg.Msg.(proto.Message)
		if !ok {
			return gerror.Newf("msg is not pb.RemoteReqMsg, msg: %v", msg)
		}
		glog.Debugf(ctx, "recv client msg, path: %s, msg: %v", msg.Path, pbmsg)
		if err := s.SendDataMsg(pbmsg, msg.Path); err != nil {
			return gerror.Wrap(err, "send data msg error")
		}
	}

	return nil
}

func (s *Session) SendDataMsg(msg proto.Message, path string) error {
	req := &pb.ClientMsg{
		Path: path,
		Msg:  &anypb.Any{},
	}
	if err := anypb.MarshalFrom(req.Msg, msg, proto.MarshalOptions{}); err != nil {
		return gerror.Newf("marshal req error, err: %v", err)
	}
	s.Request(s.sessionInfo.RolePid, req)
	return nil
}

func (s *Session) OnHandleServerMessage(ctx context.Context, msg *pb.ServerMsg) error {
	// 解析响应消息
	pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
	if err != nil {
		return gerror.Wrap(err, "unmarshal rsp error, err: %v")
	}
	if err := s.endpoint.SendMsg(pbmsg); err != nil {
		return gerror.Wrap(err, "send rsp error, err: %v")
	}
	return nil
}

// Terminate 终止会话
func (s *Session) Terminate(ctx context.Context, err error) {
	glog.Infof(ctx, "Session terminating: reason: %v, role_id: %d", err, s.sessionInfo.RoleID)
	s.state = StateDisconnected
	// 关闭网络连接
	if s.endpoint != nil {
		s.endpoint.Conn().Close()
	}
	if s.sessionInfo.RolePid != nil {
		msg := &pb.ReqAccountLogout{}
		s.SendDataMsg(msg, util.GetObjectName(msg))
	}

}

// updateLastActive 更新最后活跃时间
func (s *Session) updateLastActive() {
	if s.sessionInfo != nil {
		s.sessionInfo.LastActive = time.Now()
	}
}

// GetSessionInfo 获取会话信息
func (s *Session) GetSessionInfo() *SessionInfo {
	return s.sessionInfo
}

// IsAuthenticated 是否已认证
func (s *Session) IsAuthenticated() bool {
	return s.state == StateAuthenticated && s.sessionInfo.RoleID != 0
}
