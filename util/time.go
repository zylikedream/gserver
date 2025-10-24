package util

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

func DayRefreshTime(ctx context.Context, tm time.Time) time.Time {
	dayRefreshHour := g.Cfg().MustGet(ctx, "server.day_refresh").Int()
	refreshTime := gtime.NewFromTime(tm).StartOfDay().Add(time.Duration(dayRefreshHour) * time.Hour)
	if tm.After(refreshTime.Time) {
		return refreshTime.Add(gtime.D).Time
	}
	return refreshTime.Time
}

func IsSameDay(ctx context.Context, tm1 time.Time, tm2 time.Time) bool {
	return DayRefreshTime(ctx, tm1).Equal(DayRefreshTime(ctx, tm2))
}

func IsCrossDay(ctx context.Context, tm1 time.Time, tm2 time.Time) bool {
	return !IsSameDay(ctx, tm1, tm2)
}
