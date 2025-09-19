package logic

import (
	"gserver/core/gxynet/message"
	"time"
)

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

	SysMsgKick      = "kick"
	SysMsgHeartbeat = "heartbeat"
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

// Helper函数

// CreateErrorResponse 创建错误响应
func CreateErrorResponse(code int, message string) *ErrorResponse {
	return &ErrorResponse{
		Code:    code,
		Message: message,
	}
}

// CreateLoginResponse 创建登录响应
func CreateLoginResponse(success bool, playerID string, message string) *LoginResponse {
	return &LoginResponse{
		Success:  success,
		PlayerID: playerID,
		Message:  message,
	}
}

// CreateHeartbeatResponse 创建心跳响应
func CreateHeartbeatResponse() *HeartbeatResponse {
	return &HeartbeatResponse{
		Timestamp: time.Now().Unix(),
	}
}

// CreateGameMessageResponse 创建游戏消息响应
func CreateGameMessageResponse(success bool, message string) *GameMessageResponse {
	return &GameMessageResponse{
		Success: success,
		Message: message,
	}
}

// CreateKickNotify 创建踢出通知
func CreateKickNotify(reason string) *KickNotify {
	return &KickNotify{
		Reason: reason,
	}
}

// CreateKickSessionResponse 创建踢出会话响应
func CreateKickSessionResponse(success bool) *KickSessionResponse {
	return &KickSessionResponse{
		Success: success,
	}
}

// IsValidClientMessageType 检查是否是有效的客户端消息类型
func IsValidClientMessageType(msgType string) bool {
	switch msgType {
	case MsgTypeLogin, MsgTypeHeartbeat, MsgTypeGameMessage:
		return true
	default:
		return false
	}
}

// IsValidSystemMessageType 检查是否是有效的系统消息类型
func IsValidSystemMessageType(msgType string) bool {
	switch msgType {
	case SysMsgKick, SysMsgHeartbeat:
		return true
	default:
		return false
	}
}

// CreateClientMessage 创建客户端消息
func CreateClientMessage(msgType string, data interface{}) *ClientMessage {
	return &ClientMessage{
		Type: msgType,
		Data: data,
	}
}

// CreateSystemMessage 创建系统消息
func CreateSystemMessage(msgType string, data interface{}) *SystemMessage {
	return &SystemMessage{
		Type: msgType,
		Data: data,
	}
}

// CreateNetworkMessage 创建网络消息
func CreateNetworkMessage(data []byte) *NetworkMessage {
	return &NetworkMessage{
		Data: data,
	}
}

// 消息编解码器配置
func GetMessageCodec() string {
	return "protobuf" // 与gate.net.toml保持一致
}

// 消息创建辅助函数
func NewClientMessage(msgType string, data interface{}) (*message.Message, error) {
	return message.NewMessage(CreateClientMessage(msgType, data), nil)
}

func NewSystemMessage(msgType string, data interface{}) (*message.Message, error) {
	return message.NewMessage(CreateSystemMessage(msgType, data), nil)
}

func NewNetworkMessage(data []byte) (*message.Message, error) {
	return message.NewMessage(CreateNetworkMessage(data), nil)
}

func NewErrorResponse(code int, msg string) (*message.Message, error) {
	return message.NewMessage(CreateErrorResponse(code, msg), nil)
}

func NewLoginResponse(success bool, playerID string, msg string) (*message.Message, error) {
	return message.NewMessage(CreateLoginResponse(success, playerID, msg), nil)
}

func NewHeartbeatResponse() (*message.Message, error) {
	return message.NewMessage(CreateHeartbeatResponse(), nil)
}

func NewGameMessageResponse(success bool, msg string) (*message.Message, error) {
	return message.NewMessage(CreateGameMessageResponse(success, msg), nil)
}

func NewKickNotify(reason string) (*message.Message, error) {
	return message.NewMessage(CreateKickNotify(reason), nil)
}

func NewKickSessionResponse(success bool) (*message.Message, error) {
	return message.NewMessage(CreateKickSessionResponse(success), nil)
}
