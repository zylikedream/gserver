package gxytimer

import (
	"fmt"
	"time"
)

type Once struct {
	Name  string
	After time.Duration
}

type Tick struct {
	Name     string
	Interval time.Duration
}

func NewTick(name string, tick time.Duration) *Tick {
	return &Tick{
		Name:     name,
		Interval: tick,
	}
}

var SecondTick = &Tick{
	Name:     "second_tick",
	Interval: time.Second,
}

var MinuteTick = &Tick{
	Name:     "minute_tick",
	Interval: time.Second * 60,
}

type Cron struct {
	Name    string
	Pattern string
}

func NewCron(name, pattern string) *Cron {
	return &Cron{
		Name:    name,
		Pattern: pattern,
	}
}

const (
	DAY_REFRESH_HOUR = 5 // 5点刷新

	CRON_NAME_DAY_REFRESH    = "day_refresh"
	CRON_NAME_WEEK_REFRESH   = "week_refresh"
	CRON_NAME_MONTH_REFRESH  = "month_refresh"
	CRON_NAME_MINUTE_REFRESH = "minute_refresh"
)

var DayRefresh = &Cron{
	Name:    CRON_NAME_DAY_REFRESH,
	Pattern: fmt.Sprintf("0 0 %d * * *", DAY_REFRESH_HOUR),
}

// 周刷新点
var WeekRefresh = &Cron{
	Name:    CRON_NAME_WEEK_REFRESH,
	Pattern: fmt.Sprintf("0 0 %d * * mon", DAY_REFRESH_HOUR),
}

// 月刷新点
var MonthRefresh = &Cron{
	Name:    CRON_NAME_MONTH_REFRESH,
	Pattern: fmt.Sprintf("0 0 %d 1 * *", DAY_REFRESH_HOUR),
}

// 分钟刷新点
var MinuteRefresh = &Cron{
	Name:    CRON_NAME_MINUTE_REFRESH,
	Pattern: "0 0 * * * *",
}
