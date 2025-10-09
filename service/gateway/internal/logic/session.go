package logic

import (
	"context"
	"encoding/json"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"
	"gserver/protocol/pb"
	"gserver/service/role"

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
}

func NewSession(ep endpoint.Endpoint) *Session {
	return &Session{
		endpoint: ep,
	}
}

func (s *Session) Receive(ctx actor.Context) {
	switch msg := ctx.Message().(type) {
	case *actor.Started:
		s.Init()
	case *actor.Stopped:
		s.Terminate("normal")
	case *message.Message:
		if err := s.OnHandleClientMessage(ctx, msg); err != nil {
			glog.Errorf(context.Background(), "handle client message error, err: %v", err)
		}
	case *pb.ServerMsg:
		if err := s.OnHandleServerMessage(ctx, msg); err != nil {
			glog.Errorf(context.Background(), "handle server message error, err: %v", err)
		}
	case *actor.DeadLetterResponse:
		if msg.Target == s.sessionInfo.RolePid {
			s.Terminate("dead letter")
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
	glog.Infof(context.Background(), "Session initialized: remote: %s", s.endpoint.Conn().RemoteAddr())

	// 可以在这里进行初始化，如启动心跳等
	return nil
}

// OnHandleMessage 处理异步消息
func (s *Session) OnHandleClientMessage(ctx actor.Context, msg *message.Message) error {
	s.updateLastActive()
	switch msg.Type {
	case message.MESSGE_TYPE_FIRST_PACKET:
		firstPacket := &pb.ReqHandShake{}
		if err := json.Unmarshal(msg.Payload, firstPacket); err != nil {
			glog.Errorf(context.Background(), "unmarshal first packet error, err: %v", err)
			return err
		}
		s.sessionInfo.AccountUid = firstPacket.AccountUid
		roleID, err := role.RoleService().GetRoleIDByAccount(firstPacket.AccountUid)
		if err != nil {
			glog.Errorf(context.Background(), "get role id error, err: %v", err)
			return err
		}
		rolePid, err := role.RoleService().GetRole(roleID)
		if err != nil {
			glog.Errorf(context.Background(), "get role pid error, err: %v", err)
			return err
		}
		s.sessionInfo.RolePid = rolePid

		ctx.Watch(rolePid)
		s.state = StateAuthenticated
		ctx.Send(s.sessionInfo.RolePid, firstPacket)
	case message.MESSAGE_TYPE_DATA_PACKET:
		// 转发消息给角色actor
		pbmsg, ok := msg.Msg.(proto.Message)
		if !ok {
			glog.Errorf(context.Background(), "msg is not pb.RemoteReqMsg, msg: %v", msg)
			return nil
		}
		req := &pb.ClientMsg{}
		if err := anypb.MarshalFrom(req.Msg, pbmsg, proto.MarshalOptions{}); err != nil {
			glog.Errorf(context.Background(), "marshal req error, err: %v", err)
			return err
		}

		ctx.Send(s.sessionInfo.RolePid, req)
	}

	return nil
}

func (s *Session) OnHandleServerMessage(ctx actor.Context, msg *pb.ServerMsg) error {
	// 解析响应消息
	pbmsg, err := anypb.UnmarshalNew(msg.GetMsg(), proto.UnmarshalOptions{})
	if err != nil {
		glog.Errorf(context.Background(), "unmarshal rsp error, err: %v", err)
		return err
	}
	if err := s.endpoint.SendMsg(pbmsg); err != nil {
		glog.Errorf(context.Background(), "send rsp error, err: %v", err)
		return err
	}
	return nil
}

// OnTerminate 终止处理
func (s *Session) Terminate(reason string) {
	glog.Infof(context.Background(), "Session terminating: reason: %v", reason)

	s.state = StateDisconnected
	// 关闭网络连接
	if s.endpoint != nil {
		s.endpoint.Conn().Close()
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
	return s.state == StateAuthenticated && s.sessionInfo.RoleID != ""
}
