package gxylimit

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func TestBucketInitialBurstAndExhaustion(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	bucket, err := newBucket(Config{Rate: 2, Burst: 3}, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if !bucket.Allow() {
			t.Fatalf("Allow %d rejected inside burst", i+1)
		}
	}
	if bucket.Allow() {
		t.Fatal("Allow accepted beyond burst")
	}
}

func TestBucketRefillsAndCapsAtBurst(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	bucket, err := newBucket(Config{Rate: 2, Burst: 2}, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	if !bucket.Allow() {
		t.Fatal("unexpected initial token behavior")
	}
	if !bucket.Allow() {
		t.Fatal("unexpected initial token behavior")
	}
	if bucket.Allow() {
		t.Fatal("unexpected initial token behavior")
	}
	clock.Advance(500 * time.Millisecond)
	if !bucket.Allow() {
		t.Fatal("expected exactly one refilled token")
	}
	if bucket.Allow() {
		t.Fatal("expected exactly one refilled token")
	}
	clock.Advance(10 * time.Second)
	if !bucket.Allow() {
		t.Fatal("refill must provide burst tokens")
	}
	if !bucket.Allow() {
		t.Fatal("refill must provide burst tokens")
	}
	if bucket.Allow() {
		t.Fatal("refill must cap at burst")
	}
}

func TestBucketRefillsAtSubunitRate(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	bucket, err := newBucket(Config{Rate: 0.5, Burst: 1}, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	if !bucket.Allow() || bucket.Allow() {
		t.Fatal("unexpected initial token behavior")
	}
	clock.Advance(2 * time.Second)
	if !bucket.Allow() || bucket.Allow() {
		t.Fatal("expected exactly one token after two seconds")
	}
}

func TestNewBucketRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{name: "zero rate", config: Config{Rate: 0, Burst: 1}},
		{name: "negative rate", config: Config{Rate: -1, Burst: 1}},
		{name: "NaN rate", config: Config{Rate: math.NaN(), Burst: 1}},
		{name: "positive infinite rate", config: Config{Rate: math.Inf(1), Burst: 1}},
		{name: "negative infinite rate", config: Config{Rate: math.Inf(-1), Burst: 1}},
		{name: "zero burst", config: Config{Rate: 1, Burst: 0}},
		{name: "negative burst", config: Config{Rate: 1, Burst: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewBucket(tt.config); err == nil {
				t.Fatal("NewBucket returned nil error")
			}
		})
	}
}

func TestBucketConcurrentAllowHonorsBurst(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	bucket, err := newBucket(Config{Rate: 1, Burst: 7}, clock.Now)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 64
	start := make(chan struct{})
	var admitted atomic.Int64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			<-start
			if bucket.Allow() {
				admitted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := admitted.Load(); got != 7 {
		t.Fatalf("successful Allow calls = %d, want 7", got)
	}
}
