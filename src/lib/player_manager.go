package lib

import (
	"context"
	"sync"

	"gserver/core/gxyactor"

	"google.golang.org/protobuf/proto"
)

type playerManager struct {
	mu      sync.RWMutex
	players map[int64]gxyactor.PID
}

var globalPM = &playerManager{
	players: make(map[int64]gxyactor.PID),
}

// RegisterPlayer 注册在线玩家到当前节点
func RegisterPlayer(roleID int64, pid gxyactor.PID) {
	globalPM.mu.Lock()
	defer globalPM.mu.Unlock()
	globalPM.players[roleID] = pid
}

// UnregisterPlayer 从当前节点注销玩家
func UnregisterPlayer(roleID int64) {
	globalPM.mu.Lock()
	defer globalPM.mu.Unlock()
	delete(globalPM.players, roleID)
}

// SendToAll 给当前节点所有在线玩家发送消息
func SendToAll(msg proto.Message) {
	globalPM.mu.RLock()
	defer globalPM.mu.RUnlock()
	for _, pid := range globalPM.players {
		gxyactor.Send(context.Background(), pid, msg)
	}
}

// ForEachPlayer 遍历当前节点所有在线玩家
func ForEachPlayer(fn func(roleID int64, pid gxyactor.PID)) {
	globalPM.mu.RLock()
	defer globalPM.mu.RUnlock()
	for roleID, pid := range globalPM.players {
		fn(roleID, pid)
	}
}

// OnlineCount 当前节点在线玩家数
func OnlineCount() int {
	globalPM.mu.RLock()
	defer globalPM.mu.RUnlock()
	return len(globalPM.players)
}
