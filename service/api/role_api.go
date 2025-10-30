package api

import "time"

const (
	FriendNotifyTypeAdd = iota
	FriendNotifyTypeDel
	FriendNotifyTypeApplyRecv
)

type PFriendNotify struct {
	Type       int32     `json:"type"`
	FriendID   int64     `json:"friendID"`
	NotifyTime time.Time `json:"notify_time"`
}

type FriendNotify struct {
	NotifyList []PFriendNotify `json:"notify_list"`
}
