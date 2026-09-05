package rolelib

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymetrics"
	"gserver/core/gxymodule"
	"gserver/core/gxymq"
	"gserver/src/lib"

	"github.com/cockroachdb/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const roleNotifyTopicPrefix = "gserver:notify:role:"

// 可替换函数变量:测试注入 fake(编译期安全,非 gomonkey;ADR-0001)。
var (
	actorNodeInstance = func() string {
		app := gxyactor.ActorApp()
		if app == nil {
			return ""
		}
		return app.NodeInstanceName()
	}
	roleLocateNode = func(ctx context.Context, roleID int64) (string, error) {
		owner, err := gxyactor.GetActorOwner(ctx, lib.ROLE_ACTOR_TYPE, strconv.FormatInt(roleID, 10))
		return owner.NodeID, err
	}
	getLocalActor    = gxyactor.GetLocalActor
	getLocalActorAll = gxyactor.GetLocalActorAll
	localSend        = gxyactor.LocalSend
	mqSubscribe      = func(ctx context.Context, topic string, handler func(ctx context.Context, msg string) error) error {
		return gxymq.MessageQueue().Subscribe(ctx, topic, handler)
	}
	mqPublish = func(ctx context.Context, topic, msg string) error {
		return gxymq.MessageQueue().Publish(ctx, topic, msg)
	}
)

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
	r.nodeInstanceName = actorNodeInstance()
	return nil
}

func (r *RoleNotify) OnModStart(ctx context.Context) error {
	return mqSubscribe(ctx, roleNotifyTopic(r.nodeInstanceName), r.handleNotify)
}

func (r *RoleNotify) handleNotify(ctx context.Context, raw string) error {
	msgType := "unknown"
	result := "error"
	defer func() {
		gxymetrics.RoleNotifyConsume.WithLabelValues(msgType, result).Inc()
	}()
	msg := &roleNotifyMsg{}
	if err := json.Unmarshal([]byte(raw), msg); err != nil {
		return errors.Wrap(err, "role notify unmarshal")
	}
	if msg.TargetRoleID <= 0 || msg.Msg == nil {
		return errors.Newf("role notify invalid payload, target: %d", msg.TargetRoleID)
	}
	pbmsg, err := msg.Msg.UnmarshalNew()
	if err != nil {
		return errors.Wrap(err, "role notify any unmarshal")
	}
	msgType = protoMessageName(pbmsg)
	if err := notifyLocal(ctx, msg.TargetRoleID, pbmsg); err != nil {
		return err
	}
	result = "ok"
	return nil
}

func notifyLocal(ctx context.Context, targetRoleID int64, msg proto.Message) error {
	pid := getLocalActor(lib.ROLE_ACTOR_TYPE, strconv.FormatInt(targetRoleID, 10))
	if pid == nil {
		gxylog.Debug(ctx, "role notify target not local online", gxylog.Num("roleID", targetRoleID))
		return nil
	}
	return localSend(ctx, pid, &OnRoleNotifyMsg{Msg: msg})
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
	nodeInstanceName, err := roleLocateNode(ctx, targetRoleID)
	if nodeInstanceName == "" {
		gxylog.Debug(ctx, "role notify target offline", gxylog.Num("roleID", targetRoleID), gxylog.Err(err))
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "offline", "offline").Inc()
		return nil
	}
	if nodeInstanceName == actorNodeInstance() {
		if err := notifyLocal(ctx, targetRoleID, msg); err != nil {
			gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "error", "local").Inc()
			return err
		}
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "ok", "local").Inc()
		return nil
	}
	anyMsg := &anypb.Any{}
	if err := anypb.MarshalFrom(anyMsg, msg, proto.MarshalOptions{}); err != nil {
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "error", "remote").Inc()
		return errors.Wrap(err, "role notify marshal")
	}
	payload, err := json.Marshal(&roleNotifyMsg{
		TargetRoleID: targetRoleID,
		Msg:          anyMsg,
		CreatedAt:    time.Now().Unix(),
	})
	if err != nil {
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "error", "remote").Inc()
		return errors.Wrap(err, "role notify json marshal")
	}
	if err := mqPublish(ctx, roleNotifyTopic(nodeInstanceName), string(payload)); err != nil {
		gxymetrics.RoleNotifyPublish.WithLabelValues(msgType, "error", "remote").Inc()
		return errors.Wrap(err, "role notify publish")
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

func GetRolePid(RoleID int64) gxyactor.PID {
	return getLocalActor(lib.ROLE_ACTOR_TYPE, strconv.FormatInt(RoleID, 10))
}

func NotifyLocalAll(ctx context.Context, msg proto.Message) error {
	pids := getLocalActorAll(lib.ROLE_ACTOR_TYPE)
	for _, pid := range pids {
		_ = localSend(ctx, pid, &OnRoleNotifyMsg{Msg: msg})
	}
	return nil
}
