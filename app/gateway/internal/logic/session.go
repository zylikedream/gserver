package logic

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"

	"ergo.services/ergo/act"
	"github.com/gogf/gf/v2/os/glog"
)

const (
	SESSION_MSG_STOP = "stop"
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
	PlayerID    string    // 玩家ID（认证后才有）
	ConnectTime time.Time // 连接时间
	LastActive  time.Time // 最后活跃时间
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
func (s *Session) HandleMessage(from gxyactor.PID, msg any) error {
	s.updateLastActive()
	return nil
}

// OnHandleCall 处理同步调用
func (s *Session) HandleCall(from gxyactor.PID, ref gxyactor.Ref, request any) (any, error) {
	s.updateLastActive()
	return nil, nil
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
func (s *Session) sendToClient(data any) error {
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
		return s.sessionInfo.PlayerID
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
		s.sessionInfo.PlayerID = playerID
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
	return s.state == StateAuthenticated && s.sessionInfo.PlayerID != ""
}
