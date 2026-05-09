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
	localRoles       sync.Map // roleID → *roleEntry
	localGuildRoles  sync.Map // roleID → *guildRoleEntry
	sidecarCancel    context.CancelFunc
	sidecarMutex     sync.Mutex
)

type roleEntry struct {
	lobbyID int64
	pid     gxyactor.PID
}

type guildRoleEntry struct {
	guildID int64
	pid     gxyactor.PID
}

func RegisterRoleGuildChat(roleID int64, guildID int64, pid gxyactor.PID) {
	localGuildRoles.Store(roleID, &guildRoleEntry{guildID: guildID, pid: pid})
}

func UnregisterRoleGuildChat(roleID int64) {
	localGuildRoles.Delete(roleID)
}

func StartSidecar(ctx context.Context) {
	sidecarMutex.Lock()
	defer sidecarMutex.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	sidecarCancel = cancel

	go runSidecar(ctx)
	glog.Info(ctx, "[chat] sidecar started")
}

func StopSidecar() {
	sidecarMutex.Lock()
	defer sidecarMutex.Unlock()
	if sidecarCancel != nil {
		sidecarCancel()
	}
}

func RegisterLocalRole(roleID int64, lobbyID int64, pid gxyactor.PID) {
	localRoles.Store(roleID, &roleEntry{lobbyID: lobbyID, pid: pid})
}

func UnregisterLocalRole(roleID int64) {
	localRoles.Delete(roleID)
}

func runSidecar(ctx context.Context) {
	sub := gxyredis.Redis().PSubscribe(ctx, "chat:pub:*")
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
			handleSidecarMsg(ctx, msg)
		}
	}
}

func handleSidecarMsg(ctx context.Context, msg *redis.Message) {
	channel := msg.Channel

	if strings.HasPrefix(channel, "chat:pub:lobby:") {
		// 世界频道：只推给本节点中同大厅的角色
		parts := strings.SplitN(channel, ":", 4)
		if len(parts) < 4 {
			return
		}
		lobbyID, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return
		}
		chatMsg, err := jsonToMsg(msg.Payload)
		if err != nil {
			glog.Warningf(ctx, "[chat] sidecar parse lobby msg error: %v", err)
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
		// 系统频道：推给本节点所有角色
		chatMsg, err := jsonToMsg(msg.Payload)
		if err != nil {
			glog.Warningf(ctx, "[chat] sidecar parse system msg error: %v", err)
			return
		}
		notify := &pb.NotifySystemChat{Message: chatMsg}
		localRoles.Range(func(key, value any) bool {
			entry := value.(*roleEntry)
			gxyactor.LocalSend(entry.pid, notify)
			return true
		})

	} else if strings.HasPrefix(channel, "chat:pub:private:") {
		// 私聊：只推给本节点中的目标角色
		parts := strings.SplitN(channel, ":", 4)
		if len(parts) < 4 {
			return
		}
		targetID, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return
		}
		chatMsg, err := jsonToMsg(msg.Payload)
		if err != nil {
			glog.Warningf(ctx, "[chat] sidecar parse private msg error: %v", err)
			return
		}
		if entry, ok := localRoles.Load(targetID); ok {
			e := entry.(*roleEntry)
			gxyactor.LocalSend(e.pid, &pb.NotifyPrivateChat{
				Message: chatMsg,
			})
		}

	} else if strings.HasPrefix(channel, "chat:pub:guild:") {
		parts := strings.SplitN(channel, ":", 4)
		if len(parts) < 4 {
			return
		}
		guildID, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil {
			return
		}
		chatMsg, err := jsonToMsg(msg.Payload)
		if err != nil {
			glog.Warningf(ctx, "[chat] sidecar parse guild msg error: %v", err)
			return
		}
		notify := &pb.NotifyChannelChat{
				ChannelType: 2, // GuildChannel
				ChannelId:   guildID,
				SenderId:    chatMsg.Sender.GetRoleId(),
				Content:     chatMsg.Content,
				Timestamp:   chatMsg.Timestamp,
			}
		localGuildRoles.Range(func(key, value any) bool {
			entry := value.(*guildRoleEntry)
			if entry.guildID == guildID {
				gxyactor.LocalSend(entry.pid, notify)
			}
			return true
		})
	}
}
