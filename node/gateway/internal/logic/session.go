package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"
	"gserver/protocol/pb"
	"gserver/service/role"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/glog"
)

const (
	SESSION_MSG_STOP   = "stop"
	SESSION_MSG_CLIENT = "client" // 客户端消息
)

// SessionState 会话状态
type SessionState int

const (
	StateConnected      SessionState = iota // 已连接
	StateHandshaking                        // 握手阶段
	StateAuthenticating                     // 认证阶段
	StateAuthenticated                      // 已认证
	StateDisconnecting                      // 断开中
	StateDisconnected                       // 已断开
)

// SessionInfo 会话信息
type SessionInfo struct {
	AccountUid  string    // 账号ID
	RoleID      string    // 玩家ID（认证后才有）
	ConnectTime time.Time // 连接时间
	LastActive  time.Time // 最后活跃时间
	RolePid     gen.PID
}

// Session 会话Actor，继承自ActorBase
type Session struct {
	act.Actor
	connID      string            // 连接ID
	endpoint    endpoint.Endpoint // 网络端点
	state       SessionState      // 会话状态
	sessionInfo *SessionInfo      // 会话信息
}

// NewSession 创建会话
// OnInit Actor初始化
func (s *Session) Init(args ...any) error {
	if len(args) < 2 {
		return fmt.Errorf("session init args error, expect connID and endpoint, got %d", len(args))
	}
	s.connID = args[0].(string)
	s.endpoint = args[1].(endpoint.Endpoint)
	s.endpoint.SetData(s.PID())
	s.state = StateConnected
	s.sessionInfo = &SessionInfo{
		ConnectTime: time.Now(),
		LastActive:  time.Now(),
	}
	glog.Infof(context.Background(), "Session initialized: %s, remote: %s", s.connID, s.endpoint.Conn().RemoteAddr())

	// 可以在这里进行初始化，如启动心跳等
	return nil
}

// OnHandleMessage 处理异步消息
func (s *Session) OnHandleMessage(from gxyactor.PID, rawmsg gxyactor.ActorMessage) error {
	s.updateLastActive()
	switch rawmsg.Name {
	case gxyactor.MsgClient:
		msg, _ := rawmsg.Data.(message.Message)
		switch msg.Type {
		case message.MESSGE_TYPE_FIRST_PACKET:
			firstPacket := pb.ReqHandShake{}
			if err := json.Unmarshal(msg.Payload, &firstPacket); err != nil {
				glog.Errorf(context.Background(), "unmarshal first packet error, connID: %s, err: %v", s.connID, err)
				return err
			}
			s.sessionInfo.AccountUid = firstPacket.AccountUid
			roleID, err := role.RoleService().GetRoleIDByAccount(firstPacket.AccountUid)
			if err != nil {
				glog.Errorf(context.Background(), "get role id error, connID: %s, err: %v", s.connID, err)
				return err
			}
			rolePid, err := role.RoleService().SpawnRole(roleID)
			if err != nil {
				glog.Errorf(context.Background(), "get role pid error, connID: %s, err: %v", s.connID, err)
				return err
			}
			s.sessionInfo.RolePid = rolePid
			s.Monitor(rolePid)
		case message.MESSAGE_TYPE_DATA_PACKET:
			rolePid := s.sessionInfo.RolePid
			if rolePid.Node == "" {
				glog.Errorf(context.Background(), "role pid is nil, connID: %s", s.connID)
				return fmt.Errorf("role pid is nil")
			}
			// 转发消息给角色actor
			if err := s.Send(rolePid, rawmsg); err != nil {
				glog.Errorf(context.Background(), "send message to role error, roleID: %s, err: %v", s.sessionInfo.RoleID, err)
				return err
			}
		}
	}

	return nil
}

// OnTerminate 终止处理
func (s *Session) Terminate(reason error) {
	glog.Infof(context.Background(), "Session terminating: %s, reason: %v", s.connID, reason)

	// 关闭网络连接
	if s.endpoint != nil {
		s.endpoint.Conn().Close()
	}
}

// sendToClient 发送消息给客户端
func (s *Session) SendToClient(data any) error {
	if s.endpoint == nil {
		return fmt.Errorf("endpoint is nil")
	}

	msg, err := message.NewMessage(data, nil)
	if err != nil {
		return fmt.Errorf("failed to create message: %w", err)
	}

	return s.endpoint.SendMsg(msg)
}

// updateLastActive 更新最后活跃时间
func (s *Session) updateLastActive() {
	if s.sessionInfo != nil {
		s.sessionInfo.LastActive = time.Now()
	}
}

// GetConnectionID 获取连接ID
func (s *Session) GetConnectionID() string {
	return s.connID
}

// GetPlayerID 获取玩家ID
func (s *Session) GetPlayerID() string {
	if s.sessionInfo != nil {
		return s.sessionInfo.RoleID
	}
	return ""
}

// GetState 获取会话状态
func (s *Session) GetState() SessionState {
	return s.state
}

// SetState 设置会话状态
func (s *Session) SetState(state SessionState) {
	s.state = state
}

// SetPlayerID 设置玩家ID（认证成功后调用）
func (s *Session) SetPlayerID(playerID string) {
	if s.sessionInfo != nil {
		s.sessionInfo.RoleID = playerID
	}
	s.state = StateAuthenticated
	glog.Infof(context.Background(), "Session %s authenticated: PlayerID=%s", s.connID, playerID)
}

// GetSessionInfo 获取会话信息
func (s *Session) GetSessionInfo() *SessionInfo {
	return s.sessionInfo
}

// IsAuthenticated 是否已认证
func (s *Session) IsAuthenticated() bool {
	return s.state == StateAuthenticated && s.sessionInfo.RoleID != ""
}
