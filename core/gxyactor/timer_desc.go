package gxyactor

import (
	"fmt"
	"time"
)

type Once struct {
	Name    string
	EndTime time.Duration
}

type Tick struct {
	Name string
	Tick time.Duration
}

func NewTick(name string, tick time.Duration) *Tick {
	return &Tick{
		Name: name,
		Tick: tick,
	}
}

var SecondTick = &Tick{
	Name: "second_tick",
	Tick: time.Second,
}

var MinuteTick = &Tick{
	Name: "minute_tick",
	Tick: time.Second * 60,
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
	DAY_REFRESH = 5 // 5点刷新
)

var DayZero = &Cron{
	Name:    "day_zero",
	Pattern: "0 0 0 * * *",
}

var DayRefresh = &Cron{
	Name:    "day_refresh",
	Pattern: fmt.Sprintf("0 0 %d * * *", DAY_REFRESH),
}

// 周零点
var WeekZero = &Cron{
	Name:    "week_zero",
	Pattern: "0 0 0 * * mon",
}

// 周刷新点
var WeekRefresh = &Cron{
	Name:    "week_refresh",
	Pattern: fmt.Sprintf("0 0 %d * * mon", DAY_REFRESH),
}

// 月零点
var MonthZero = &Cron{
	Name:    "month_zero",
	Pattern: "0 0 0 1 * *",
}

// 月刷新点
var MonthRefresh = &Cron{
	Name:    "month_refresh",
	Pattern: fmt.Sprintf("0 0 %d 1 * *", DAY_REFRESH),
}
