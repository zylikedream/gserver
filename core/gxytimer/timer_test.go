package gxytimer

import (
	"context"
	"testing"
	"time"
)

// ========== NewTimer ==========

func TestNewTimer_Init(t *testing.T) {
	tm := NewTimer()
	if tm == nil {
		t.Fatal("expected non-nil timer")
	}
	if len(tm.timerInfos) != 0 {
		t.Fatalf("expected empty timerInfos, got %d", len(tm.timerInfos))
	}
	if tm.timer == nil {
		t.Fatal("expected non-nil gtimer")
	}
	if tm.cron == nil {
		t.Fatal("expected non-nil gcron")
	}
}

// ========== NewTick / NewCron ==========

func TestNewTick(t *testing.T) {
	tick := NewTick("test_tick", 5*time.Second)
	if tick.Name != "test_tick" {
		t.Fatalf("expected test_tick, got %s", tick.Name)
	}
	if tick.Interval != 5*time.Second {
		t.Fatalf("expected 5s, got %v", tick.Interval)
	}
}

func TestNewCron(t *testing.T) {
	cron := NewCron("test_cron", "0 0 * * * *")
	if cron.Name != "test_cron" {
		t.Fatalf("expected test_cron, got %s", cron.Name)
	}
	if cron.Pattern != "0 0 * * * *" {
		t.Fatalf("expected pattern, got %s", cron.Pattern)
	}
}

func TestPredefinedTicks(t *testing.T) {
	if SecondTick.Interval != time.Second {
		t.Fatalf("expected 1s, got %v", SecondTick.Interval)
	}
	if SecondTick.Name != "second_tick" {
		t.Fatalf("expected second_tick, got %s", SecondTick.Name)
	}
	if MinuteTick.Interval != 60*time.Second {
		t.Fatalf("expected 60s, got %v", MinuteTick.Interval)
	}
}

// ========== newTimerInfo constructors ==========

func TestNewTickTimerInfo(t *testing.T) {
	tick := &Tick{Name: "t", Interval: 3 * time.Second}
	fn := func(ctx context.Context, info TimerActiveInfo) {}
	info := newTickTimerInfo(tick, fn)
	if info.name != "t" || info.duration != 3*time.Second {
		t.Fatalf("unexpected: name=%s duration=%v", info.name, info.duration)
	}
	if info.tick != tick {
		t.Fatal("tick pointer mismatch")
	}
}

func TestNewOnceTimerInfo(t *testing.T) {
	once := &Once{Name: "o", After: 10 * time.Second}
	fn := func(ctx context.Context, info TimerActiveInfo) {}
	info := newOnceTimerInfo(once, fn)
	if info.name != "o" || info.duration != 10*time.Second {
		t.Fatalf("unexpected: name=%s duration=%v", info.name, info.duration)
	}
	if info.Once != once {
		t.Fatal("once pointer mismatch")
	}
}

func TestNewCronTimerInfo_Duration(t *testing.T) {
	cron := &Cron{Name: "c", Pattern: "0 * * * * *"} // every minute
	fn := func(ctx context.Context, info TimerActiveInfo) {}
	info := newCronTimerInfo(cron, fn)
	if info.name != "c" {
		t.Fatalf("expected c, got %s", info.name)
	}
	if info.cron != cron {
		t.Fatal("cron pointer mismatch")
	}
	// 每分钟执行，间隔应为 60s
	if info.duration != 60*time.Second {
		t.Fatalf("expected 60s for minutely cron, got %v", info.duration)
	}
}

// ========== getCronDuration ==========

func TestGetCronDuration_EveryMinute(t *testing.T) {
	d := getCronDuration("0 * * * * *")
	if d != 60*time.Second {
		t.Fatalf("expected 60s, got %v", d)
	}
}

func TestGetCronDuration_EveryFiveMinutes(t *testing.T) {
	d := getCronDuration("0 */5 * * * *")
	if d != 5*time.Minute {
		t.Fatalf("expected 5m, got %v", d)
	}
}

func TestGetCronDuration_EveryTenSeconds(t *testing.T) {
	d := getCronDuration("*/10 * * * * *")
	if d != 10*time.Second {
		t.Fatalf("expected 10s, got %v", d)
	}
}

func TestGetCronDuration_Invalid(t *testing.T) {
	d := getCronDuration("invalid pattern")
	if d != 0 {
		t.Fatalf("expected 0 for invalid pattern, got %v", d)
	}
}

// ========== getIntervalCount ==========

func TestGetIntervalCount(t *testing.T) {
	tm := NewTimer()
	if c := tm.getIntervalCount(10*time.Second, 60*time.Second); c != 6 {
		t.Fatalf("expected 6, got %d", c)
	}
	if c := tm.getIntervalCount(30*time.Second, 90*time.Second); c != 3 {
		t.Fatalf("expected 3, got %d", c)
	}
}

func TestGetIntervalCount_Zero(t *testing.T) {
	tm := NewTimer()
	if c := tm.getIntervalCount(10*time.Second, 0); c != 0 {
		t.Fatalf("expected 0, got %d", c)
	}
}

func TestGetIntervalCount_Truncation(t *testing.T) {
	tm := NewTimer()
	// 95 / 30 = 3 (integer division)
	if c := tm.getIntervalCount(30*time.Second, 95*time.Second); c != 3 {
		t.Fatalf("expected 3, got %d", c)
	}
}

// ========== AddTick / AddOnce / AddCron (registration only) ==========

func TestAddTick_RegistersInfo(t *testing.T) {
	tm := NewTimer()
	ctx := context.Background()
	called := make(chan struct{}, 1)
	tm.AddTick(ctx, &Tick{Name: "my_tick", Interval: 50 * time.Millisecond}, func(ctx context.Context, info TimerActiveInfo) {
		called <- struct{}{}
	})
	info, ok := tm.timerInfos["my_tick"]
	if !ok {
		t.Fatal("expected timerInfo to be registered")
	}
	if info.name != "my_tick" || info.tick == nil {
		t.Fatal("unexpected timerInfo")
	}
	// Verify the timer actually fires
	select {
	case <-called:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("tick did not fire within 200ms")
	}
	tm.Stop(ctx)
}

func TestAddOnce_RegistersInfo(t *testing.T) {
	tm := NewTimer()
	ctx := context.Background()
	called := make(chan struct{}, 1)
	tm.AddOnce(ctx, &Once{Name: "my_once", After: 30 * time.Millisecond}, func(ctx context.Context, info TimerActiveInfo) {
		called <- struct{}{}
	})
	info, ok := tm.timerInfos["my_once"]
	if !ok {
		t.Fatal("expected timerInfo to be registered")
	}
	if info.name != "my_once" || info.Once == nil {
		t.Fatal("unexpected timerInfo")
	}
	select {
	case <-called:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("once did not fire within 200ms")
	}
	tm.Stop(ctx)
}

func TestAddCron_RegistersInfo(t *testing.T) {
	tm := NewTimer()
	ctx := context.Background()
	called := make(chan struct{}, 1)
	err := tm.AddCron(ctx, &Cron{Name: "my_cron", Pattern: "* * * * * *"}, func(ctx context.Context, info TimerActiveInfo) {
		called <- struct{}{}
	})
	if err != nil {
		t.Fatal(err)
	}
	info, ok := tm.timerInfos["my_cron"]
	if !ok {
		t.Fatal("expected timerInfo to be registered")
	}
	if info.name != "my_cron" || info.cron == nil {
		t.Fatal("unexpected timerInfo")
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("cron did not fire within 2s")
	}
	tm.Stop(ctx)
}

func TestAddCron_InvalidPattern(t *testing.T) {
	tm := NewTimer()
	err := tm.AddCron(context.Background(), &Cron{Name: "bad", Pattern: "not valid"}, nil)
	if err == nil {
		t.Fatal("expected error for invalid pattern")
	}
}

// ========== Cancel ==========

func TestCancel_RemovesInfo(t *testing.T) {
	tm := NewTimer()
	ctx := context.Background()
	tm.AddTick(ctx, &Tick{Name: "t1", Interval: time.Minute}, func(ctx context.Context, info TimerActiveInfo) {})
	if len(tm.timerInfos) != 1 {
		t.Fatalf("expected 1 info, got %d", len(tm.timerInfos))
	}
	tm.Cancel(ctx, "t1")
	if len(tm.timerInfos) != 0 {
		t.Fatalf("expected 0 after cancel, got %d", len(tm.timerInfos))
	}
}

func TestCancel_Unknown(t *testing.T) {
	tm := NewTimer()
	tm.Cancel(context.Background(), "nonexistent")
	// should not panic
}

// ========== Stop ==========

func TestStop_ClearsAll(t *testing.T) {
	tm := NewTimer()
	ctx := context.Background()
	tm.AddTick(ctx, &Tick{Name: "a", Interval: time.Minute}, func(ctx context.Context, info TimerActiveInfo) {})
	tm.AddOnce(ctx, &Once{Name: "b", After: time.Minute}, func(ctx context.Context, info TimerActiveInfo) {})
	tm.Stop(ctx)
	if len(tm.timerInfos) != 0 {
		t.Fatalf("expected 0 after stop, got %d", len(tm.timerInfos))
	}
}

// ========== TimerActiveInfo ==========

func TestTimerActiveInfo_Fields(t *testing.T) {
	now := time.Now()
	info := TimerActiveInfo{
		Name:          "test",
		Time:          now,
		Duration:      5 * time.Second,
		IntervalCount: 3,
	}
	if info.Name != "test" || info.Duration != 5*time.Second || info.IntervalCount != 3 {
		t.Fatal("unexpected fields")
	}
}

// ========== RestoreCron (pure logic verification) ==========

func TestRestoreCron_CallsFuncForPastCron(t *testing.T) {
	tm := NewTimer()
	ctx := context.Background()

	called := make(chan TimerActiveInfo, 1)
	// Pattern that fires every second
	err := tm.AddCron(ctx, &Cron{Name: "test_cron", Pattern: "*/1 * * * * *"}, func(ctx context.Context, info TimerActiveInfo) {
		called <- info
	})
	if err != nil {
		t.Fatal(err)
	}

	// Restore with a time 2 seconds in the past — should trigger
	pastTime := time.Now().Add(-2 * time.Second)
	tm.RestoreCron(ctx, pastTime)

	select {
	case info := <-called:
		if info.Name != "test_cron" {
			t.Fatalf("expected test_cron, got %s", info.Name)
		}
		if info.IntervalCount < 1 {
			t.Fatalf("expected at least 1 missed interval, got %d", info.IntervalCount)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("RestoreCron did not fire callback for past cron")
	}
	tm.Stop(ctx)
}

func TestRestoreCron_SkipFutureCron(t *testing.T) {
	tm := NewTimer()
	ctx := context.Background()

	called := make(chan TimerActiveInfo, 1)
	err := tm.AddCron(ctx, &Cron{Name: "no_call", Pattern: "*/1 * * * * *"}, func(ctx context.Context, info TimerActiveInfo) {
		called <- info
	})
	if err != nil {
		t.Fatal(err)
	}

	// Restore with a future time — should NOT trigger
	futureTime := time.Now().Add(time.Hour)
	tm.RestoreCron(ctx, futureTime)

	select {
	case <-called:
		t.Fatal("RestoreCron should not fire for future cron time")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
	tm.Stop(ctx)
}

// ========== Predefined cron patterns ==========

func TestDayRefresh_CronPattern(t *testing.T) {
	if DayRefresh.Name != CRON_NAME_DAY_REFRESH {
		t.Fatalf("expected %s, got %s", CRON_NAME_DAY_REFRESH, DayRefresh.Name)
	}
	d := getCronDuration(DayRefresh.Pattern)
	if d != 24*time.Hour {
		t.Fatalf("expected 24h for day refresh, got %v", d)
	}
}

func TestWeekRefresh_CronPattern(t *testing.T) {
	if WeekRefresh.Name != CRON_NAME_WEEK_REFRESH {
		t.Fatalf("expected %s, got %s", CRON_NAME_WEEK_REFRESH, WeekRefresh.Name)
	}
}

func TestMinuteRefresh_CronPattern(t *testing.T) {
	d := getCronDuration(MinuteRefresh.Pattern)
	if d != time.Hour {
		t.Fatalf("expected 1h for minute refresh pattern, got %v", d)
	}
}
