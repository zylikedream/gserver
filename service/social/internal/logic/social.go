package logic

import (
	"context"
	"fmt"
	"gserver/core/gxyredis"
	"gserver/protocol/pb"
	"gserver/service/social/api"
	"time"

	"github.com/gogf/gf/v2/encoding/gjson"
)

const (
	ROLE_ONLINE_EXPIRE = 8 * time.Hour
)

type roleOnlineInfo struct {
	RoleID     int64       `json:"role_id"`
	OnlineTime time.Time   `json:"online_time"`
	Pid        pb.ActorPid `json:"pid"`
}

type SocialController struct {
}

// NewSocialController 创建社交控制器实例
func NewSocialController() *SocialController {
	return &SocialController{}
}

func (s *SocialController) getOnlineKey(roleID int64) string {
	return fmt.Sprintf("role:online:%d", roleID)
}

func (s *SocialController) RoleOnline(ctx context.Context, req *api.RoleOnlineReq) (*api.RoleOnlineRes, error) {
	// redis查询角色是否在线
	roleID := req.RoleID
	roleOnlineInfo := &roleOnlineInfo{
		RoleID:     roleID,
		OnlineTime: time.Now(),
		Pid: pb.ActorPid{
			Address: req.Pid.Address,
			Id:      req.Pid.Id,
		},
	}
	// 序列化角色在线信息
	onlineData, err := gjson.Marshal(roleOnlineInfo)
	if err != nil {
		return nil, err
	}
	// 存储角色在线信息到redis
	err = gxyredis.GetRedis().Set(ctx, s.getOnlineKey(roleID), onlineData, ROLE_ONLINE_EXPIRE).Err()
	if err != nil {
		return nil, err
	}
	return &api.RoleOnlineRes{}, nil
}

func (s *SocialController) RoleOffline(ctx context.Context, req *api.RoleOfflineReq) (*api.RoleOfflineRes, error) {
	roleID := req.RoleID
	// 从redis删除角色在线信息
	err := gxyredis.GetRedis().Del(ctx, s.getOnlineKey(roleID)).Err()
	if err != nil {
		return nil, err
	}
	return &api.RoleOfflineRes{}, nil
}

func (s *SocialController) OnRoleEvent(ctx context.Context, msg string) error {

	return nil
}
