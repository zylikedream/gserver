package api

import "github.com/gogf/gf/v2/frame/g"

// FriendInfo 好友信息结构
type FriendInfo struct {
	RoleID     string `bson:"role_id"`
	FriendID   string `bson:"friend_id"`
	Source     string `bson:"source"` //来源
	FriendTime int64  `bson:"friend_time"`
	State      int32  `bson:"state"` // 1-申请中, 2-好友
}

// 请求参数结构
type ApplyFriendReq struct {
	g.Meta   `path:"/apply" method:"POST"`
	RoleID   string `json:"role_id" v:"required"`
	FriendID string `json:"friend_id" v:"required"`
	Source   string `json:"source"`
}

type ApplyFriendRes struct {
	ApplyNew FriendInfo `json:"apply_new"`
}

type DealApplyReq struct {
	g.Meta    `path:"/deal_apply" method:"POST"`
	RoleID    string `json:"role_id" v:"required"`
	ApplyerID string `json:"applyer_id" v:"required"`
	Deal      int32  `json:"deal" v:"required"` // 1-接受, 2-拒绝
}

type DealApplyRes struct {
	FriendNew FriendInfo `json:"friend_new"`
}

type DeleteFriendReq struct {
	g.Meta   `path:"/delete" method:"POST"`
	RoleID   string `json:"role_id" v:"required"`
	FriendID string `json:"friend_id" v:"required"`
}

type DeleteFriendRes struct {
}
type GetFriendListReq struct {
	g.Meta `path:"/get_friend_list" method:"POST"`
	RoleID string `json:"role_id" v:"required"`
}

type GetFriendListRes struct {
	FriendList    []FriendInfo `json:"friend_list"`
	ApplySendList []FriendInfo `json:"apply_send_list"`
	ApplyRecvList []FriendInfo `json:"apply_recv_list"`
}
