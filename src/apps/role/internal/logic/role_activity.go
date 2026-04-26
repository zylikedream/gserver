package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"gserver/src/apps/role/internal/event"
	"gserver/src/apps/world/server"
	"time"

	"github.com/ahmetb/go-linq"
	"github.com/gogf/gf/v2/os/glog"
	"gorm.io/datatypes"
)

type RoleActivityPersistState struct {
	RolePersistState
	Activitys datatypes.JSON `gorm:"column:activitys;type:jsonb"`
}

func (RoleActivityPersistState) TableName() string { return "role_activity" }

func (r *RoleActivityPersistState) GetIndexes() []string {
	return []string{"update_at"}
}

type RoleActivity struct {
	RoleModule
	RoleActivityPersistState
	activitysMap map[int32]server.ActivityData
}

func (r *RoleActivity) PersistState() IPersistState {
	return &r.RoleActivityPersistState
}

func (r *RoleActivity) OnModStart(ctx context.Context) error {
	if len(r.Activitys) > 0 {
		json.Unmarshal(r.Activitys, &r.activitysMap)
	}
	if r.activitysMap == nil {
		r.activitysMap = make(map[int32]server.ActivityData)
	}
	r.updateActivity(ctx)
	return nil
}

func (r *RoleActivity) SyncToPersist() {
	data, _ := json.Marshal(r.activitysMap)
	r.Activitys = data
}

func (r *RoleActivity) updateActivity(ctx context.Context) {
	nowActs := server.GetActs()
	now := time.Now()
	for _, act := range r.activitysMap {
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
		if _, ok := r.activitysMap[act.ID]; ok { // 服务器打开了
			continue
		}
		r.onActivityOpen(ctx, act)
	}
}

func (r *RoleActivity) onActivityOpen(ctx context.Context, act *server.ActivityData) {
	glog.Debugf(ctx, "onActivityOpen %v", act)
	r.Role.eventBus.Publish(MakeActivityOpenEvent(act.ID), act)
	r.Role.eventBus.Publish(event.EVENT_ACTIVITY_OPEN, act)
	delete(r.activitysMap, act.ID)
}

func (r *RoleActivity) onActivityClosed(ctx context.Context, act *server.ActivityData) {
	glog.Debugf(ctx, "onActivityclose %v", act)
	r.Role.eventBus.Publish(MakeActivityCloseEvent(act.ID), act)
	r.Role.eventBus.Publish(event.EVENT_ACTIVITY_CLOSE, act)
	r.activitysMap[act.ID] = *act
}

func MakeActivityOpenEvent(id int32) event.EventType {
	return event.EventType(fmt.Sprintf("%s:%d", event.EVENT_ACTIVITY_OPEN, id))
}

func MakeActivityCloseEvent(id int32) event.EventType {
	return event.EventType(fmt.Sprintf("%s:%d", event.EVENT_ACTIVITY_CLOSE, id))
}
