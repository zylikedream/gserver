package logic

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxynet/endpoint"
	"gserver/core/gxynet/message"

	"ergo.services/ergo/act"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
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
	switch msg := msg.(type) {
	case *ClientMessage:
		return s.handleClientMessage(msg)
	case *NetworkMessage:
		return s.handleNetworkMessage(msg)
	case *SystemMessage:
		return s.handleSystemMessage(msg)
	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}
}

// OnHandleCall 处理同步调用
func (s *Session) HandleCall(from gxyactor.PID, ref gxyactor.Ref, request any) (any, error) {
	s.updateLastActive()
	switch req := request.(type) {
	case *GetSessionInfoRequest:
		return s.sessionInfo, nil
	case *KickSessionRequest:
		s.handleKick(req.Reason)
		return &KickSessionResponse{Success: true}, nil
	default:
		return nil, fmt.Errorf("unknown request type: %T", request)
	}
}

// OnTerminate 终止处理
func (s *Session) Terminate(reason error) {
	glog.Infof(context.Background(), "Session terminating: %s, reason: %v", s.connID, reason)

	// 关闭网络连接
	if s.endpoint != nil {
		s.endpoint.Conn().Close()
	}
}

// handleClientMessage 处理客户端消息
func (s *Session) handleClientMessage(msg *ClientMessage) error {
	glog.Debugf(context.Background(), "Session %s received client message: %s", s.connID, msg.Type)

	switch msg.Type {
	case MsgTypeLogin:
		return s.handleLogin(msg.Data)
	case MsgTypeHeartbeat:
		return s.handleHeartbeat()
	case MsgTypeGameMessage:
		return s.handleGameMessage(msg.Data)
	default:
		return fmt.Errorf("unknown client message type: %s", msg.Type)
	}
}

// handleNetworkMessage 处理网络消息（从网络层收到的消息）
func (s *Session) handleNetworkMessage(msg *NetworkMessage) error {
	// 转发给客户端
	return s.sendToClient(msg.Data)
}

// handleSystemMessage 处理系统消息
func (s *Session) handleSystemMessage(msg *SystemMessage) error {
	switch msg.Type {
	case SysMsgKick:
		s.handleKick(msg.Data.(string))
	case SysMsgStop:
		return gerror.New(msg.Data.(string))
	case SysMsgHeartbeat:
		// 心跳响应
		return s.sendHeartbeatResponse()
	default:
		return fmt.Errorf("unknown system message type: %s", msg.Type)
	}
	return nil
}

// handleLogin 处理登录消息
func (s *Session) handleLogin(data any) error {
	if s.state != StateHandshaking && s.state != StateConnected {
		return fmt.Errorf("invalid state for login: %v", s.state)
	}

	loginReq, ok := data.(*LoginRequest)
	if !ok {
		return fmt.Errorf("invalid login request data")
	}

	glog.Infof(context.Background(), "Session %s login request: PlayerID=%s", s.connID, loginReq.PlayerID)

	// 进入认证阶段
	s.state = StateAuthenticating
	s.sessionInfo.PlayerID = loginReq.PlayerID

	// TODO: 这里实现认证逻辑
	// 1. 验证token
	// 2. 检查重复登录
	// 3. 获取玩家数据
	// 4. 设置认证状态

	// 模拟认证成功
	s.SetPlayerID(loginReq.PlayerID)

	// 发送登录响应给客户端
	return s.sendToClient(&LoginResponse{
		Success:  true,
		PlayerID: loginReq.PlayerID,
		Message:  "Login successful",
	})
}

// handleHeartbeat 处理心跳
func (s *Session) handleHeartbeat() error {
	glog.Debugf(context.Background(), "Session %s heartbeat", s.connID)
	return s.sendHeartbeatResponse()
}

// handleGameMessage 处理游戏消息
func (s *Session) handleGameMessage(data any) error {
	if s.state != StateAuthenticated {
		return fmt.Errorf("session not authenticated")
	}

	glog.Debugf(context.Background(), "Session %s game message: %T", s.connID, data)

	// TODO: 这里实现游戏消息处理逻辑
	// 1. 验证消息格式
	// 2. 权限检查
	// 3. 转发到游戏服（后续实现）
	// 4. 返回响应给客户端

	// 模拟处理成功
	return s.sendToClient(&GameMessageResponse{
		Success: true,
		Message: "Message processed",
	})
}

// handleKick 处理踢出
func (s *Session) handleKick(reason string) {
	glog.Infof(context.Background(), "Session %s kicked: %s", s.connID, reason)

	// 发送踢出消息给客户端
	s.sendToClient(&KickNotify{
		Reason: reason,
	})

	// 延迟关闭连接，让客户端有时间处理踢出消息
	time.AfterFunc(2*time.Second, func() {
		s.Terminate(fmt.Errorf("kicked: %s", reason))
	})
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

// sendHeartbeatResponse 发送心跳响应
func (s *Session) sendHeartbeatResponse() error {
	return s.sendToClient(&HeartbeatResponse{
		Timestamp: time.Now().Unix(),
	})
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
