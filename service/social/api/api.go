package api

import (
	"gserver/protocol/pb"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	TOPIC_SOCIAL_FRIEND_NOTIFY = "social_friend_notify"
)

type RoleOnlineReq struct {
	g.Meta `path:"/online" method:"POST"`
	RoleID int64       `json:"role_id"`
	Pid    pb.ActorPid `json:"pid"`
}

type RoleOnlineRes struct {
}

type RoleOfflineReq struct {
	g.Meta `path:"/online" method:"POST"`
	RoleID int64 `json:"role_id"`
}

type RoleOfflineRes struct {
}
