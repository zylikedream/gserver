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
	"gserver/service/role/roleconsts"

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
	StateConnected     SessionState = iota // 已连接
	StateAuthenticated                     // 已认证
	StateDisconnected                      // 已断开
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
	endpoint    endpoint.Endpoint // 网络端点
	state       SessionState      // 会话状态
	sessionInfo *SessionInfo      // 会话信息
}

func NewSession() *Session {
	return &Session{}
}

// NewSession 创建会话
// OnInit Actor初始化
func (s *Session) Init(args ...any) error {
	if len(args) < 1 {
		return fmt.Errorf("session init args error, expect connID and endpoint, got %d", len(args))
	}
	s.endpoint = args[0].(endpoint.Endpoint)
	s.endpoint.SetData(s.PID())
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
func (s *Session) OnHandleMessage(from gxyactor.PID, rawmsg gxyactor.ActorMessage) error {
	s.updateLastActive()
	switch rawmsg.Name {
	case gxyactor.MsgClientReq:
		msg, _ := rawmsg.Data.(message.Message)
		switch msg.Type {
		case message.MESSGE_TYPE_FIRST_PACKET:
			firstPacket := pb.ReqHandShake{}
			if err := json.Unmarshal(msg.Payload, &firstPacket); err != nil {
				glog.Errorf(context.Background(), "unmarshal first packet error, err: %v", err)
				return err
			}
			s.sessionInfo.AccountUid = firstPacket.AccountUid
			roleID, err := role.RoleService().GetRoleIDByAccount(firstPacket.AccountUid)
			if err != nil {
				glog.Errorf(context.Background(), "get role id error, err: %v", err)
				return err
			}
			rolePid, err := role.RoleService().SpawnRole(roleID, roleconsts.ROLE_SPAWN_REASON_FRIST_PACKET)
			if err != nil {
				glog.Errorf(context.Background(), "get role pid error, err: %v", err)
				return err
			}
			s.sessionInfo.RolePid = rolePid
			s.Monitor(rolePid)
			s.state = StateAuthenticated
		case message.MESSAGE_TYPE_DATA_PACKET:
			rolePid := s.sessionInfo.RolePid
			if rolePid.Node == "" {
				glog.Error(context.Background(), "role pid is nil")
				return fmt.Errorf("role pid is nil")
			}
			// 转发消息给角色actor
			if err := s.Send(rolePid, rawmsg); err != nil {
				glog.Errorf(context.Background(), "send message to role error, roleID: %s, err: %v", s.sessionInfo.RoleID, err)
				return err
			}
		}
	case gxyactor.MsgServerRsp:
		rsp := rawmsg.Data
		err := s.endpoint.SendMsg(rsp)
		if err != nil {
			glog.Errorf(context.Background(), "send message to client error, err: %v", err)
			return err
		}
	}

	return nil
}

// OnTerminate 终止处理
func (s *Session) Terminate(reason error) {
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
