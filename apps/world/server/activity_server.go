package server

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxytimer"
	"gserver/gameconfig"
	"gserver/util"
	"gserver/util/ets"
	"time"

	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/os/gtime"
)

var actEts = ets.NewETS("activity")

const (
	ACTIVITY_SERVER      = "activity_server"
	ACTIVITY_CHECK_TIMER = "check_timer"
)

type ActivityData struct {
	ID        int32     `json:"id"`
	Name      string    `json:"name"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

type ActivityServer struct {
	*gxyactor.ActorBase
}

func NewActivityServer() *ActivityServer {
	ctx := gxylog.NewContext(context.Background(), ACTIVITY_SERVER)
	return &ActivityServer{
		ActorBase: gxyactor.NewActorBase(ctx, nil),
	}
}

func (a *ActivityServer) DelayInit(ctx context.Context) error {
	return a.updateActivity(ctx)
}

func (a *ActivityServer) updateActivity(ctx context.Context) error {
	acts := []*ActivityData{}     // 正在开启的活动
	minEndTime := util.MAX_TIME   // 已开启活动的最早结束时间
	minStartTime := util.MAX_TIME // 未开启活动的最早开启时间
	actTable := gameconfig.GameConfig().TbActivity
	now := gtime.Now()
	for _, act := range actTable.GetDataList() {
		startTime, err := gtime.StrToTime(act.StartTime)
		if err != nil {
			glog.Warning(ctx, "act time %s format err %s", act.StartTime, err)
			continue
		}
		endTime, err := gtime.StrToTime(act.EndTime)
		if err != nil {
			glog.Warning(ctx, "act time %s format err %s", act.StartTime, err)
			continue
		}
		if startTime.Before(now) && endTime.After(now) {
			acts = append(acts, &ActivityData{
				ID:        act.Id,
				Name:      act.Name,
				StartTime: startTime.Time,
				EndTime:   endTime.Time,
			})
			if endTime.Time.Before(minEndTime) {
				minEndTime = endTime.Time
			}
		} else if startTime.After(now) {
			if startTime.Time.Before(minStartTime) {
				minStartTime = startTime.Time
			}
		}
	}
	minCheckTime := util.If(minStartTime.Before(minEndTime), minStartTime, minEndTime)
	a.Timer().AddOnce(ctx, &gxytimer.Once{
		Name:  ACTIVITY_CHECK_TIMER,
		After: minCheckTime.Sub(now.Time),
	}, func(ctx context.Context, info gxytimer.TimerActiveInfo) {
		a.updateActivity(ctx)
	})
	actEts.Set("acts", acts)
	return nil
}

func (a *ActivityServer) HandleMessage(ctx context.Context, msg any) error {
	return nil
}

func GetActs() []*ActivityData {
	return actEts.Get("acts", []*ActivityData{}).([]*ActivityData)
}
