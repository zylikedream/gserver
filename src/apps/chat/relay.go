package chat

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"gserver/core/gxyactor"
	"gserver/core/gxyredis"
	"gserver/protocol/pb"

	"github.com/gogf/gf/v2/os/glog"
	"github.com/redis/go-redis/v9"
)

var (
	localRoles  sync.Map // roleID → *roleEntry
	relayCancel context.CancelFunc
	relayMutex  sync.Mutex
)

type roleEntry struct {
	lobbyID int64
	pid     gxyactor.PID
}

func StartRelay(ctx context.Context) {
	relayMutex.Lock()
	defer relayMutex.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	relayCancel = cancel

	go runRelay(ctx)
	glog.Info(ctx, "[chat] relay started")
}

func StopRelay() {
	relayMutex.Lock()
	defer relayMutex.Unlock()
	if relayCancel != nil {
		relayCancel()
	}
}

func RegisterRole(roleID int64, lobbyID int64, pid gxyactor.PID) {
	localRoles.Store(roleID, &roleEntry{lobbyID: lobbyID, pid: pid})
}

func UnregisterRole(roleID int64) {
	localRoles.Delete(roleID)
}

func runRelay(ctx context.Context) {
	sub := gxyredis.Redis().PSubscribe(ctx, "chat:pub:lobby:*", "chat:pub:system")
	defer sub.Close()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			handlePubSub(ctx, msg)
		}
	}
}

func handlePubSub(ctx context.Context, msg *redis.Message) {
	chatMsg, err := jsonToMsg(msg.Payload)
	if err != nil {
		glog.Warningf(ctx, "[chat] parse pubsub msg error: %v", err)
		return
	}

	channel := msg.Channel
	if strings.HasPrefix(channel, "chat:pub:lobby:") {
		parts := strings.SplitN(channel, ":", 4)
		if len(parts) < 4 {
			return
		}
		lobbyID, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return
		}
		notify := &pb.NotifyWorldChat{Message: chatMsg}
		localRoles.Range(func(key, value any) bool {
			entry := value.(*roleEntry)
			if entry.lobbyID == lobbyID {
				gxyactor.LocalSend(entry.pid, notify)
			}
			return true
		})
	} else if channel == "chat:pub:system" {
		notify := &pb.NotifySystemChat{Message: chatMsg}
		localRoles.Range(func(key, value any) bool {
			entry := value.(*roleEntry)
			gxyactor.LocalSend(entry.pid, notify)
			return true
		})
	}
}
