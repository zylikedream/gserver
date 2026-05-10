package chat

import (
	"errors"
	"time"
)

// IChannel 定义频道的差异化行为
type IChannel interface {
	ChannelType() string         // "world", "guild"
	RingBufferSize() int         // 消息保留上限
	SaveInterval() time.Duration // >0 启用定时存盘
	TableName() string           // 存盘表名（SaveInterval>0时需要）
	CanWrite(roleID int64, content string) error
	CanJoin(roleID int64) bool
}

// WorldChannel 世界频道
type WorldChannel struct{}

func (WorldChannel) ChannelType() string         { return "world" }
func (WorldChannel) RingBufferSize() int         { return 200 }
func (WorldChannel) SaveInterval() time.Duration { return 0 }
func (WorldChannel) TableName() string           { return "" }
func (WorldChannel) CanWrite(_ int64, content string) error {
	if content == "" {
		return errors.New("消息不能为空")
	}
	return nil
}
func (WorldChannel) CanJoin(_ int64) bool { return true }

// GuildChannel 公会频道
type GuildChannel struct{}

func (GuildChannel) ChannelType() string         { return "guild" }
func (GuildChannel) RingBufferSize() int         { return 500 }
func (GuildChannel) SaveInterval() time.Duration { return 600 * time.Second }
func (GuildChannel) TableName() string           { return "guild_chat_log" }
func (GuildChannel) CanWrite(_ int64, content string) error {
	if content == "" {
		return errors.New("消息不能为空")
	}
	return nil
}
func (GuildChannel) CanJoin(_ int64) bool { return true }

// channelRegistry channelType → IChannel
// channelType 枚举值定义见策划配置表 chatchannel.xlsx channel_type 列：
//   1=世界 2=私聊 3=系统 4=公会
var channelRegistry = map[int32]IChannel{
	1: WorldChannel{},
	4: GuildChannel{},
}

func GetChannel(channelType int32) (IChannel, bool) {
	c, ok := channelRegistry[channelType]
	return c, ok
}
