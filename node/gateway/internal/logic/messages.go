package logic

import (
	"gserver/core/gxynet/message"
)

var codec message.IMessageCodec

func init() {
	codec, _ = message.NewMessageCodec(message.MESSAGE_JSON)
}

// 消息类型定义
type (

	// ClientMessage 客户端消息
	ClientMessage struct {
		Type string      `json:"type"` // 消息类型
		Data interface{} `json:"data"` // 消息数据
	}

	// NetworkMessage 网络消息（网关发给客户端）
	NetworkMessage struct {
		Data []byte `json:"data"` // 二进制数据
	}

	// SystemMessage 系统消息
	SystemMessage struct {
		Type string      `json:"type"` // 系统消息类型
		Data interface{} `json:"data"` // 消息数据
	}

	// LoginRequest 登录请求
	LoginRequest struct {
		PlayerID string `json:"player_id"` // 玩家ID
		Token    string `json:"token"`     // 认证令牌
	}

	// LoginResponse 登录响应
	LoginResponse struct {
		Success  bool   `json:"success"`   // 是否成功
		PlayerID string `json:"player_id"` // 玩家ID
		Message  string `json:"message"`   // 响应消息
	}

	// HeartbeatResponse 心跳响应
	HeartbeatResponse struct {
		Timestamp int64 `json:"timestamp"` // 时间戳
	}

	// GameMessageResponse 游戏消息响应
	GameMessageResponse struct {
		Success bool   `json:"success"` // 是否成功
		Message string `json:"message"` // 响应消息
	}

	// KickNotify 踢出通知
	KickNotify struct {
		Reason string `json:"reason"` // 踢出原因
	}

	// GetSessionInfoRequest 获取会话信息请求
	GetSessionInfoRequest struct{}

	// KickSessionRequest 踢出会话请求
	KickSessionRequest struct {
		Reason string `json:"reason"` // 踢出原因
	}

	// KickSessionResponse 踢出会话响应
	KickSessionResponse struct {
		Success bool `json:"success"` // 是否成功
	}

	// ErrorResponse 错误响应
	ErrorResponse struct {
		Code    int    `json:"code"`    // 错误码
		Message string `json:"message"` // 错误消息
	}
)

// 消息类型常量
const (
	MsgTypeLogin       = "login"
	MsgTypeHeartbeat   = "heartbeat"
	MsgTypeGameMessage = "game_message"
	MsgTypeError       = "error"
)

// 错误码定义
const (
	ErrCodeSuccess          = 0
	ErrCodeInvalidRequest   = 1001
	ErrCodeNotAuthenticated = 1002
	ErrCodeInvalidState     = 1003
	ErrCodeInternalError    = 1004
	ErrCodePlayerNotFound   = 1005
	ErrCodeDuplicateLogin   = 1006
)

type ActorMessage struct {
	MsgType string `json:"msg_type"` // 消息类型
	Data    any    `json:"data"`     // 消息数据
}

func NewActorMessage(msgType string, data any) *ActorMessage {
	return &ActorMessage{
		MsgType: msgType,
		Data:    data,
	}
}
