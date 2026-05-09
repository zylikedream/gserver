package chat

import "time"

type ChatPrivateMessage struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	MinRoleID int64     `gorm:"column:min_role_id;not null;index:idx_chat_pm_pair_time"`
	MaxRoleID int64     `gorm:"column:max_role_id;not null;index:idx_chat_pm_pair_time"`
	SenderID  int64     `gorm:"column:sender_id;not null"`
	Content   string    `gorm:"column:content;not null;type:text"`
	CreatedAt time.Time `gorm:"column:created_at;not null;index:idx_chat_pm_pair_time"`
}

func (ChatPrivateMessage) TableName() string { return "chat_private_message" }

type ChatSystemMessage struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	Content   string    `gorm:"column:content;not null;type:text"`
	CreatedAt time.Time `gorm:"column:created_at;not null;index"`
}

func (ChatSystemMessage) TableName() string { return "chat_system_message" }
