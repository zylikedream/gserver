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
	RoleID      string       // 玩家ID（认证后才有）
	ConnectTime time.Time    // 连接时间
	LastActive  time.Time    // 最后活跃时间
	RolePid     gxyactor.PID // 角色PID
}

// Session 会话Actor，继承自ActorBase
type Session struct {
	endpoint    endpoint.Endpoint // 网络端点
	state       SessionState      // 会话状态
	sessionInfo *SessionInfo      // 会话信息
	actx        actor.Context     // 会话上下文
	ctx         context.Context   // 会话上下文
}

func NewSession(ep endpoint.Endpoint) *Session {
	return &Session{
		endpoint: ep,
		ctx:      gxylog.NewContext(context.Background(), "session"),
	}
}

func (s *Session) Receive(ctx actor.Context) {
	s.actx = ctx
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		if err := s.Init(); err != nil {
			glog.Errorf(s.ctx, "init session error, err: %v", err)
		}
	case *actor.Stopped:
		s.Stop("normal")
	case *message.Message:
		if err := s.OnHandleClientMessage(ctx, msg); err != nil {
			glog.Errorf(s.ctx, "handle client message error, err: %v", err)
		}
	case *pb.ServerMsg:
		if err := s.OnHandleServerMessage(ctx, msg); err != nil {
			glog.Errorf(s.ctx, "handle server message error, err: %v", err)
		}
	case *pb.ActorStop:
		s.Stop(msg.Reason)
	case *actor.Terminated:
		if msg.Who == s.sessionInfo.RolePid {
			s.sessionInfo.RolePid = nil
			s.Stop("role terminated")
		}
	}
}

// NewSession 创建会话
// OnInit Actor初始化
func (s *Session) Init() error {
	s.state = StateConnected
	s.sessionInfo = &SessionInfo{
		ConnectTime: time.Now(),
		LastActive:  time.Now(),
	}
	glog.Infof(s.ctx, "Session initialized: remote: %s", s.endpoint.Conn().RemoteAddr())

	// 可以在这里进行初始化，如启动心跳等
	return nil
}

func (s *Session) handleHandshake(msg any) error {
	firstpacket, ok := msg.(*pb.ReqHandShake)
	if !ok {
		glog.Errorf(s.ctx, "first packet is not pb.ReqHandShake, msg: %v", msg)
		return nil
	}

	s.sessionInfo.AccountUid = firstpacket.AccountUid
	roleID, err := role.GetRoleIDByAccount(firstpacket.AccountUid)
	if err != nil {
		glog.Errorf(s.ctx, "get role id error, err: %v", err)
		return err
	}
	rolePid, err := role.RoleService().GetRole(roleID)
	if err != nil {
		glog.Errorf(s.ctx, "get role pid error, err: %v", err)
		return err
	}
	s.sessionInfo.RolePid = rolePid

	s.actx.Watch(rolePid)
	s.ctx = gxylog.WithValue(s.ctx, gxylog.ContextKeyRoleID, roleID)
	s.state = StateAuthenticated
	rsp := &pb.RspHandShake{
		AccountUid: firstpacket.AccountUid,
		RoleId:     roleID,
	}
	if err := s.endpoint.SendMsg(rsp); err != nil {
		glog.Errorf(s.ctx, "send rsp error, err: %v", err)
		return err
	}
	SessionMgr().Add(roleID, s.actx.Self())
	return nil
}

// OnHandleMessage 处理异步消息
func (s *Session) OnHandleClientMessage(ctx actor.Context, msg *message.Message) error {
	s.updateLastActive()
	switch msg.Type {
	case message.MESSGE_TYPE_FIRST_PACKET:
		if err := s.handleHandshake(msg.Msg); err != nil {
			glog.Errorf(s.ctx, "handle handshake error, err: %v", err)
			s.Stop("handshake failed")
			return err
		}
	case message.MESSAGE_TYPE_DATA_PACKET:
		// 转发消息给角色actor
		pbmsg, ok := msg.Msg.(proto.Message)
		if !ok {
			glog.Errorf(s.ctx, "msg is not pb.RemoteReqMsg, msg: %v", msg)
			return nil
		}
		glog.Debugf(s.ctx, "recv client msg, path: %s, msg: %v", msg.Path, pbmsg)
		if err := s.SendDataMsg(pbmsg, msg.Path); err != nil {
			glog.Errorf(s.ctx, "send data msg error, err: %v", err)
			return err
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
		glog.Errorf(s.ctx, "marshal req error, err: %v", err)
		return err
	}
	s.actx.Request(s.sessionInfo.RolePid, req)
	return nil
}

func (s *Session) OnHandleServerMessage(ctx actor.Context, msg *pb.ServerMsg) error {
	// 解析响应消息
	pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
	if err != nil {
		glog.Errorf(s.ctx, "unmarshal rsp error, err: %v", err)
		return err
	}
	if err := s.endpoint.SendMsg(pbmsg); err != nil {
		glog.Errorf(s.ctx, "send rsp error, err: %v", err)
		return err
	}
	return nil
}

// OnTerminate 终止处理
func (s *Session) Stop(reason string) {
	if s.state == StateDisconnected {
		return
	}
	glog.Infof(s.ctx, "Session terminating: reason: %v, role_pid: %v", reason, s.sessionInfo.RolePid)

	s.state = StateDisconnected
	// 关闭网络连接
	if s.endpoint != nil {
		s.endpoint.Conn().Close()
	}
	if s.sessionInfo.RolePid != nil {
		msg := &pb.ReqAccountLogout{}
		s.SendDataMsg(msg, util.GetObjectName(msg))
	}
	s.actx.Stop(s.actx.Self())
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
	return s.state == StateAuthenticated && s.sessionInfo.RoleID != ""
}
