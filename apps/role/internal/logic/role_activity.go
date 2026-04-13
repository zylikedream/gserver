package logic

import (
	"context"
	"fmt"
	"gserver/apps/role/internal/event"
	"gserver/apps/world/server"
	"time"

	"github.com/ahmetb/go-linq"
	"github.com/gogf/gf/v2/os/glog"
)

type RoleActivityPersistState struct {
	RolePersistState `db:"inline"`
	Activitys        map[int32]server.ActivityData `db:"activitys"`
}

type RoleActivity struct {
	RoleModule `db:"inline"`
	RoleActivityPersistState
}

func (r *RoleActivity) PersistState() IPersistState {
	return &r.RoleActivityPersistState
}

func (r *RoleActivity) OnModStart(ctx context.Context) error {
	r.updateActivity(ctx)
	return nil
}

func (r *RoleActivity) updateActivity(ctx context.Context) {
	nowActs := server.GetActs()
	now := time.Now()
	for _, act := range r.Activitys {
		if act.EndTime.After(now) { // 时间到了
			r.onActivityClosed(ctx, &act)
			continue
		}
		serverClosed := linq.From(nowActs).All(func(i any) bool {
			return i.(server.ActivityData).ID != act.ID
		})
		if serverClosed { // 服务器关闭了
			r.onActivityClosed(ctx, &act)
			continue
		}
	}

	for _, act := range nowActs {
		if _, ok := r.Activitys[act.ID]; ok { // 服务器打开了
			continue
		}
		r.onActivityOpen(ctx, act)
	}
}

func (r *RoleActivity) onActivityOpen(ctx context.Context, act *server.ActivityData) {
	glog.Debugf(ctx, "onActivityOpen %v", act)
	r.Role.eventBus.Publish(MakeActivityOpenEvent(act.ID), act)
	r.Role.eventBus.Publish(event.EVENT_ACTIVITY_OPEN, act)
	delete(r.Activitys, act.ID)
}

func (r *RoleActivity) onActivityClosed(ctx context.Context, act *server.ActivityData) {
	glog.Debugf(ctx, "onActivityclose %v", act)
	r.Role.eventBus.Publish(MakeActivityCloseEvent(act.ID), act)
	r.Role.eventBus.Publish(event.EVENT_ACTIVITY_CLOSE, act)
	r.Activitys[act.ID] = *act
}

func MakeActivityOpenEvent(id int32) event.EventType {
	return event.EventType(fmt.Sprintf("%s:%d", event.EVENT_ACTIVITY_OPEN, id))
}

func MakeActivityCloseEvent(id int32) event.EventType {
	return event.EventType(fmt.Sprintf("%s:%d", event.EVENT_ACTIVITY_CLOSE, id))
}
