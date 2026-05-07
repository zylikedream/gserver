package api

import "time"

type FriendBatchItem struct {
	TargetID int64  `json:"target_id"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
}

type RolePublicData struct {
	Name        string    `gorm:"column:name"`
	Head        string    `gorm:"column:head"`
	CreateTime  time.Time `gorm:"column:create_time"`
	Level       int32     `gorm:"column:level"`
	LastLoginAt time.Time `gorm:"column:last_login_at"`
	IsOnline    bool      `gorm:"column:is_online"`
}
