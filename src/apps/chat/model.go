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

// ChatGuildMessage 公会频道消息落库(ChannelActor.save 写, loadHistory 读)。
type ChatGuildMessage struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	ChannelType int32  `gorm:"column:channel_type;not null;index:idx_chat_guild_channel_time"`
	ChannelID   int64  `gorm:"column:channel_id;not null;index:idx_chat_guild_channel_time"`
	SenderID    int64  `gorm:"column:sender_id;not null"`
	Content     string `gorm:"column:content;not null;type:text"`
	Timestamp   int64  `gorm:"column:timestamp;not null;index:idx_chat_guild_channel_time"`
}

func (ChatGuildMessage) TableName() string { return "chat_guild_message" }
