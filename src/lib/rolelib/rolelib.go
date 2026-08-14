package rolelib

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxymodule"
	"gserver/core/gxymq"
	"gserver/core/gxyredis"
	"gserver/src/lib"

	"github.com/cockroachdb/errors"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const roleNotifyTopicPrefix = "gserver:notify:role:"

type roleNotifyMsg struct {
	TargetRoleID int64      `json:"target_role_id"`
	Msg          *anypb.Any `json:"msg"`
	CreatedAt    int64      `json:"created_at"`
}

type OnRoleNotifyMsg struct {
	Msg proto.Message
}

type RoleNotify struct {
	gxymodule.ModuleBase
	nodeInstanceName string
}

func NewRoleNotify() *RoleNotify {
	return &RoleNotify{}
}

func (r *RoleNotify) OnModInit(ctx context.Context) error {
	r.nodeInstanceName = gxyactor.ActorApp().NodeInstanceName()
	return nil
}

func (r *RoleNotify) OnModStart(ctx context.Context) error {
	return gxymq.MessageQueue().Subscribe(ctx, roleNotifyTopic(r.nodeInstanceName), r.handleNotify)
}

func (r *RoleNotify) handleNotify(ctx context.Context, raw string) error {
	msgType := "unknown"
	result := "error"
	defer func() {
		gxymetrics.RoleNotifyConsume.WithLabelValues(msgType, result).Inc()
	}()
	msg := &roleNotifyMsg{}
	if err := json.Unmarshal([]byte(raw), msg); err != nil {
		return fmt.Errorf("role notify unmarshal: %w", err)
	}
	if msg.TargetRoleID <= 0 || msg.Msg == nil {
		return errors.Newf("role notify invalid payload, target: %d", msg.TargetRoleID)
	}
	pbmsg, err := msg.Msg.UnmarshalNew()
	if err != nil {
		return fmt.Errorf("role notify any unmarshal: %w", err)
	}
	msgType = protoMessageName(pbmsg)
	if err := notifyLocal(ctx, msg.TargetRoleID, pbmsg); err != nil {
		return err
	}
	result = "ok"
	return nil
}

func notifyLocal(ctx context.Context, targetRoleID int64, msg proto.Message) error {
	pid := gxyactor.GetLocalActor(lib.ROLE_ACTOR_TYPE, strconv.FormatInt(targetRoleID, 10))
	if pid == nil {
		gxylog.Debug(ctx, "role notify target not local online", gxylog.Num("roleID", targetRoleID))
		return nil
	}
	return gxyactor.LocalSend(ctx, pid, &OnRoleNotifyMsg{Msg: msg})
}

func roleNotifyTopic(nodeInstanceName string) string {
	return roleNotifyTopicPrefix + nodeInstanceName
}

func PublishRoleNotify(ctx context.Context, targetRoleID int64, msg proto.Message) error {
	msgType := protoMessageName(msg)
	if targetRoleID <= 0 || msg == nil {
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "error", "invalid").Inc()
		return nil
	}
	nodeInstanceName, err := gxyredis.Redis().Get(ctx, GetRoleLocateKey(targetRoleID)).Result()
	if err == redis.Nil || nodeInstanceName == "" {
		gxylog.Debug(ctx, "role notify target offline", gxylog.Num("roleID", targetRoleID))
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "offline", "offline").Inc()
		return nil
	}
	if nodeInstanceName == gxyactor.ActorApp().NodeInstanceName() {
		if err := notifyLocal(ctx, targetRoleID, msg); err != nil {
			gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "error", "local").Inc()
			return err
		}
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "ok", "local").Inc()
		return nil
	}
	if err != nil {
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "error", "unknown").Inc()
		return fmt.Errorf("get role notify target locate: %w", err)
	}
	anyMsg := &anypb.Any{}
	if err := anypb.MarshalFrom(anyMsg, msg, proto.MarshalOptions{}); err != nil {
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "error", "remote").Inc()
		return fmt.Errorf("role notify marshal: %w", err)
	}
	payload, err := json.Marshal(&roleNotifyMsg{
		TargetRoleID: targetRoleID,
		Msg:          anyMsg,
		CreatedAt:    time.Now().Unix(),
	})
	if err != nil {
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "error", "remote").Inc()
		return fmt.Errorf("role notify json marshal: %w", err)
	}
	if err := gxymq.MessageQueue().Publish(ctx, roleNotifyTopic(nodeInstanceName), string(payload)); err != nil {
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "error", "remote").Inc()
		return fmt.Errorf("role notify publish: %w", err)
	}
	gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "ok", "remote").Inc()
	return nil
}

func protoMessageName(msg proto.Message) string {
	if msg == nil {
		return "unknown"
	}
	return string(msg.ProtoReflect().Descriptor().Name())
}

func GetRoleLocateKey(roleID int64) string {
	return fmt.Sprintf("gserver:locate:node:actor:role:%d", roleID)
}

func GetRolePid(RoleID int64) gxyactor.PID {
	return gxyactor.GetLocalActor(lib.ROLE_ACTOR_TYPE, strconv.FormatInt(RoleID, 10))
}

func NotifyLocalAll(ctx context.Context, msg proto.Message) error {
	pids := gxyactor.GetLocalActorAll(lib.ROLE_ACTOR_TYPE)
	for _, pid := range pids {
		gxyactor.LocalSend(ctx, pid, &OnRoleNotifyMsg{Msg: msg})
	}
	return nil
}
