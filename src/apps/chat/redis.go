package chat

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"gserver/core/gxypgx"
	"gserver/core/gxyredis"
	"gserver/protocol/pb"
	"gserver/src/pkg/gameconfig"

	"github.com/redis/go-redis/v9"
)

// ===== Lua 脚本 =====

var luaJoinLobby = redis.NewScript(`-- KEYS[1] = chat:lobby:sizes
-- ARGV[1] = maxSize, ARGV[2] = roleID
local maxSize = tonumber(ARGV[1])
local roleID = ARGV[2]
local available = redis.call('ZREVRANGEBYSCORE', KEYS[1], maxSize - 1, 0, 'LIMIT', 0, 1)
local lobbyID
if #available == 0 then
    lobbyID = tostring(redis.call('INCR', 'chat:lobby:counter'))
else
    lobbyID = available[1]
end
redis.call('SADD', 'chat:lobby:' .. lobbyID, roleID)
redis.call('ZINCRBY', KEYS[1], 1, lobbyID)
redis.call('PERSIST', 'chat:lobby:' .. lobbyID)
redis.call('PERSIST', 'chat:msg:lobby:' .. lobbyID)
return lobbyID`)

var luaLeaveLobby = redis.NewScript(`-- ARGV[1] = roleID, ARGV[2] = lobbyID
local roleID = ARGV[1]
local lobbyID = ARGV[2]
redis.call('SREM', 'chat:lobby:' .. lobbyID, roleID)
redis.call('ZINCRBY', 'chat:lobby:sizes', -1, lobbyID)
local size = redis.call('SCARD', 'chat:lobby:' .. lobbyID)
if size == 0 then
    redis.call('EXPIRE', 'chat:lobby:' .. lobbyID, 259200)
    redis.call('EXPIRE', 'chat:msg:lobby:' .. lobbyID, 259200)
end
return 1`)

// ===== 大厅操作 =====

func JoinLobby(ctx context.Context, roleID int64) (int64, error) {
	maxCap := int(gameconfig.Get().TbGlobalConfig.Get().WorldChatLobbyMaxPlayers)
	cmd := luaJoinLobby.Run(ctx, gxyredis.Redis(), []string{"chat:lobby:sizes"},
		maxCap, strconv.FormatInt(roleID, 10))
	lobbyStr, err := cmd.Text()
	if err != nil {
		return 0, fmt.Errorf("chat join lobby: %w", err)
	}
	lobbyID, err := strconv.ParseInt(lobbyStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("chat parse lobby id: %w", err)
	}
	return lobbyID, nil
}

func LeaveLobby(ctx context.Context, roleID, lobbyID int64) error {
	cmd := luaLeaveLobby.Run(ctx, gxyredis.Redis(), nil,
		strconv.FormatInt(roleID, 10), strconv.FormatInt(lobbyID, 10))
	if _, err := cmd.Int(); err != nil {
		return fmt.Errorf("chat leave lobby: %w", err)
	}
	return nil
}

// ===== 私聊 (PostgreSQL) =====

func StorePrivateMsg(ctx context.Context, senderID, targetID int64, content string) (int64, error) {
	minID, maxID := sortIDs(senderID, targetID)
	msg := &ChatPrivateMessage{
		MinRoleID: minID, MaxRoleID: maxID,
		SenderID: senderID, Content: content, CreatedAt: time.Now(),
	}
	if err := gxypgx.DB().WithContext(ctx).Create(msg).Error; err != nil {
		return 0, fmt.Errorf("chat store private msg: %w", err)
	}
	return msg.CreatedAt.Unix(), nil
}

func GetPrivateHistory(ctx context.Context, roleID, friendID int64, count int) ([]*pb.PChatMsg, error) {
	minID, maxID := sortIDs(roleID, friendID)
	var msgs []ChatPrivateMessage
	err := gxypgx.DB().WithContext(ctx).
		Where("min_role_id = ? AND max_role_id = ?", minID, maxID).
		Order("created_at DESC").Limit(count).
		Find(&msgs).Error
	if err != nil {
		return nil, err
	}
	result := make([]*pb.PChatMsg, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		result = append(result, &pb.PChatMsg{
			Sender: &pb.PRolePublic{
				RoleId: m.SenderID,
			},
			Content:   m.Content,
			Timestamp: m.CreatedAt.Unix(),
		})
	}
	return result, nil
}

// ===== 系统消息 (PostgreSQL) =====

func StoreSystemMsg(ctx context.Context, content string) (int64, error) {
	msg := &ChatSystemMessage{
		Content: content, CreatedAt: time.Now(),
	}
	if err := gxypgx.DB().WithContext(ctx).Create(msg).Error; err != nil {
		return 0, fmt.Errorf("chat store system msg: %w", err)
	}
	return msg.CreatedAt.Unix(), nil
}

func GetSystemHistory(ctx context.Context, count int) ([]*pb.PChatMsg, error) {
	var msgs []ChatSystemMessage
	err := gxypgx.DB().WithContext(ctx).
		Order("created_at DESC").Limit(count).
		Find(&msgs).Error
	if err != nil {
		return nil, err
	}
	result := make([]*pb.PChatMsg, 0, len(msgs))
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		result = append(result, &pb.PChatMsg{
			Content:   m.Content,
			Timestamp: m.CreatedAt.Unix(),
		})
	}
	return result, nil
}

// ===== 工具函数 =====

func sortIDs(a, b int64) (int64, int64) {
	if a < b {
		return a, b
	}
	return b, a
}

