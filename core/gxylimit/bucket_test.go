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

// TestBucketInitialBurstAndExhaustion:新建的令牌桶起始装满 Burst 个令牌,
// 允许的次数必须正好等于 Burst,不多不少。
// 为什么需要:起始量偏大等于开局就放过一整批突发请求(打穿下游限流预期);
// 起始量偏小则冷启动即被限,表现为"服务刚起来就全部超时"。
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

// TestBucketRefillsAndCapsAtBurst:一个测试断言**两件不同的事**,分处首尾——
//   - 中段:推进 500ms(Rate 2/秒 → 恰好 1 个令牌)证明按速率补充;
//   - 末段:推进 10s(足够补满数十个令牌)证明补充量被 Burst=2 **截断**。
//
// 为什么需要:封顶只由最后一次 Allow 被拒证明——去掉上限实现,前面所有断言都全绿,
// 只有最后一行会红。不写清楚就会误以为中段也在测封顶。
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

// TestBucketRefillsAtSubunitRate:Rate 低于 1(每 2 秒才补 1 个令牌)时,
// 推进时间与补充量仍按 Rate×时长 精确对应,不因速率小而丢失补货。
// 为什么需要:补货用整数截断时,低速率配置永远补不出令牌,该限流器退化为永久拒绝。
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

// TestNewBucketRejectsInvalidConfig:Rate 非正/NaN/Inf、Burst 非正必须在构造时报错。
// 为什么需要:配置来自 YAML,NaN 与 Inf 都能解析进来——放任它们会让令牌数变成
// NaN/Inf,Allow 的比较静默失效,限流器变成永远放行。
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

// TestBucketConcurrentAllowHonorsBurst:64 个 goroutine 同时放行、时钟不前进(无补货),
// 通过数必须正好等于 Burst=7——扣减与补充必须互斥且原子。
// 为什么需要:令牌桶就是这里的并发闸门;若扣减与补充不是同一个临界区,
// 会超发(超额请求打到下游)或多发,超出的部分不会报错,只表现为下游被打穿。
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
