package logic

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gserver/core/gxylimit"
	"gserver/core/gxymetrics"

	"github.com/cockroachdb/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

type scriptedBucket struct {
	mu      sync.Mutex
	results []bool
	calls   int
}

func (b *scriptedBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.calls++
	if b.calls > len(b.results) {
		return false
	}
	return b.results[b.calls-1]
}

func (b *scriptedBucket) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

type fakeLoginTimer struct {
	ch      chan time.Time
	stopped atomic.Bool
}

func newFakeLoginTimer() *fakeLoginTimer {
	return &fakeLoginTimer{ch: make(chan time.Time, 1)}
}

func (t *fakeLoginTimer) Chan() <-chan time.Time {
	return t.ch
}

func (t *fakeLoginTimer) Stop() bool {
	return !t.stopped.Swap(true)
}

type loginAcquireResult struct {
	permit loginPermit
	err    error
}

func validLimiterConfig() LoginLimitConfig {
	return LoginLimitConfig{
		Enabled:     true,
		Rate:        1000,
		Burst:       100,
		MaxInflight: 1,
		QueueSize:   1,
		WaitTimeout: time.Minute,
	}
}

func fixedLoginNow() time.Time {
	return time.Unix(100, 0)
}

func unexpectedLoginTimer(time.Duration) loginTimer {
	panic("timer factory must not be called")
}

func resetLoginGauges(t *testing.T) {
	t.Helper()
	gxymetrics.LoginInflight.Set(0)
	gxymetrics.LoginQueueLength.Set(0)
	t.Cleanup(func() {
		gxymetrics.LoginInflight.Set(0)
		gxymetrics.LoginQueueLength.Set(0)
	})
}

func loginCounter(result string) float64 {
	return testutil.ToFloat64(gxymetrics.LoginLimitTotal.WithLabelValues(result))
}

func loginHistogramCount(t *testing.T, result string) uint64 {
	t.Helper()
	observer := gxymetrics.LoginWaitDuration.WithLabelValues(result)
	metric, ok := observer.(prometheus.Metric)
	if !ok {
		t.Fatalf("login wait histogram does not implement prometheus.Metric")
	}
	var value dto.Metric
	if err := metric.Write(&value); err != nil {
		t.Fatalf("write login wait histogram: %v", err)
	}
	return value.GetHistogram().GetSampleCount()
}

func assertLoginGauges(t *testing.T, inflight, queued float64) {
	t.Helper()
	if got := testutil.ToFloat64(gxymetrics.LoginInflight); got != inflight {
		t.Fatalf("LoginInflight = %v, want %v", got, inflight)
	}
	if got := testutil.ToFloat64(gxymetrics.LoginQueueLength); got != queued {
		t.Fatalf("LoginQueueLength = %v, want %v", got, queued)
	}
}

func TestLoginLimiterDisabledBypassesAdmission(t *testing.T) {
	resetLoginGauges(t)
	config := validLimiterConfig()
	config.Enabled = false
	bucket := &scriptedBucket{results: []bool{false}}
	okBefore := loginCounter("ok")
	okHistogramBefore := loginHistogramCount(t, "ok")

	limiter := newLoginLimiter(config, bucket, unexpectedLoginTimer, func() time.Time {
		panic("clock must not be called")
	})
	permit, err := limiter.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire disabled limiter: %v", err)
	}
	permit.Release()

	if got := bucket.callCount(); got != 0 {
		t.Fatalf("bucket calls = %d, want 0", got)
	}
	assertLoginGauges(t, 0, 0)
	if got := loginCounter("ok") - okBefore; got != 1 {
		t.Fatalf("login_limit_total{result=ok} delta = %v, want 1", got)
	}
	if got := loginHistogramCount(t, "ok") - okHistogramBefore; got != 0 {
		t.Fatalf("login_wait_duration_seconds{result=ok} count delta = %d, want 0", got)
	}
}

func TestLoginLimiterRateLimitPrecedesGate(t *testing.T) {
	resetLoginGauges(t)
	bucket := &scriptedBucket{results: []bool{false}}
	counterBefore := loginCounter("rate_limited")
	histogramBefore := loginHistogramCount(t, "rate_limited")
	limiter := newLoginLimiter(validLimiterConfig(), bucket, unexpectedLoginTimer, func() time.Time {
		panic("clock must not be called")
	})

	permit, err := limiter.acquire(context.Background())
	if permit != nil {
		t.Fatal("rate-limited acquire returned a permit")
	}
	if !errors.Is(err, ErrLoginRateLimited) {
		t.Fatalf("acquire error = %v, want ErrLoginRateLimited", err)
	}

	if got := bucket.callCount(); got != 1 {
		t.Fatalf("bucket calls = %d, want 1", got)
	}
	assertLoginGauges(t, 0, 0)
	if got := loginCounter("rate_limited") - counterBefore; got != 1 {
		t.Fatalf("login_limit_total{result=rate_limited} delta = %v, want 1", got)
	}
	if got := loginHistogramCount(t, "rate_limited") - histogramBefore; got != 0 {
		t.Fatalf("login_wait_duration_seconds{result=rate_limited} count delta = %d, want 0", got)
	}
}

func TestLoginLimiterImmediatePermitReleaseIsIdempotent(t *testing.T) {
	resetLoginGauges(t)
	bucket := &scriptedBucket{results: []bool{true}}
	counterBefore := loginCounter("ok")
	histogramBefore := loginHistogramCount(t, "ok")
	limiter := newLoginLimiter(validLimiterConfig(), bucket, unexpectedLoginTimer, fixedLoginNow)

	permit, err := limiter.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	assertLoginGauges(t, 1, 0)
	permit.Release()
	permit.Release()

	assertLoginGauges(t, 0, 0)
	if got := loginCounter("ok") - counterBefore; got != 1 {
		t.Fatalf("login_limit_total{result=ok} delta = %v, want 1", got)
	}
	if got := loginHistogramCount(t, "ok") - histogramBefore; got != 1 {
		t.Fatalf("login_wait_duration_seconds{result=ok} count delta = %d, want 1", got)
	}
}

func TestLoginLimiterQueuedRequestAcquiresAfterRelease(t *testing.T) {
	resetLoginGauges(t)
	bucket := &scriptedBucket{results: []bool{true, true}}
	registered := make(chan *fakeLoginTimer, 1)
	timerFactory := func(time.Duration) loginTimer {
		timer := newFakeLoginTimer()
		registered <- timer
		return timer
	}
	counterBefore := loginCounter("ok")
	histogramBefore := loginHistogramCount(t, "ok")
	limiter := newLoginLimiter(validLimiterConfig(), bucket, timerFactory, fixedLoginNow)

	first, err := limiter.acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	result := make(chan loginAcquireResult, 1)
	go func() {
		permit, acquireErr := limiter.acquire(context.Background())
		result <- loginAcquireResult{permit: permit, err: acquireErr}
	}()
	timer := <-registered
	assertLoginGauges(t, 1, 1)

	first.Release()
	second := <-result
	if second.err != nil {
		t.Fatalf("queued acquire: %v", second.err)
	}
	if !timer.stopped.Load() {
		t.Fatal("queued acquire timer was not stopped")
	}
	assertLoginGauges(t, 1, 0)
	second.permit.Release()

	assertLoginGauges(t, 0, 0)
	if got := loginCounter("ok") - counterBefore; got != 2 {
		t.Fatalf("login_limit_total{result=ok} delta = %v, want 2", got)
	}
	if got := loginHistogramCount(t, "ok") - histogramBefore; got != 2 {
		t.Fatalf("login_wait_duration_seconds{result=ok} count delta = %d, want 2", got)
	}
}

func TestLoginLimiterZeroQueueRejectsWhenFull(t *testing.T) {
	resetLoginGauges(t)
	config := validLimiterConfig()
	config.QueueSize = 0
	bucket := &scriptedBucket{results: []bool{true, true}}
	counterBefore := loginCounter("queue_full")
	histogramBefore := loginHistogramCount(t, "queue_full")
	limiter := newLoginLimiter(config, bucket, unexpectedLoginTimer, fixedLoginNow)

	first, err := limiter.acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	permit, err := limiter.acquire(context.Background())
	if permit != nil {
		t.Fatal("queue-full acquire returned a permit")
	}
	if !errors.Is(err, ErrLoginQueueFull) {
		t.Fatalf("second acquire error = %v, want ErrLoginQueueFull", err)
	}

	assertLoginGauges(t, 1, 0)
	if got := bucket.callCount(); got != 2 {
		t.Fatalf("bucket calls = %d, want 2", got)
	}
	if got := loginCounter("queue_full") - counterBefore; got != 1 {
		t.Fatalf("login_limit_total{result=queue_full} delta = %v, want 1", got)
	}
	if got := loginHistogramCount(t, "queue_full") - histogramBefore; got != 1 {
		t.Fatalf("login_wait_duration_seconds{result=queue_full} count delta = %d, want 1", got)
	}
	first.Release()
	assertLoginGauges(t, 0, 0)
}

func TestLoginLimiterRejectsBeyondQueueCapacity(t *testing.T) {
	resetLoginGauges(t)
	bucket := &scriptedBucket{results: []bool{true, true, true}}
	registered := make(chan *fakeLoginTimer, 1)
	timerFactory := func(time.Duration) loginTimer {
		timer := newFakeLoginTimer()
		registered <- timer
		return timer
	}
	counterBefore := loginCounter("queue_full")
	histogramBefore := loginHistogramCount(t, "queue_full")
	limiter := newLoginLimiter(validLimiterConfig(), bucket, timerFactory, fixedLoginNow)

	first, err := limiter.acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	queuedResult := make(chan loginAcquireResult, 1)
	go func() {
		permit, acquireErr := limiter.acquire(context.Background())
		queuedResult <- loginAcquireResult{permit: permit, err: acquireErr}
	}()
	<-registered
	assertLoginGauges(t, 1, 1)

	permit, err := limiter.acquire(context.Background())
	if permit != nil {
		t.Fatal("over-capacity acquire returned a permit")
	}
	if !errors.Is(err, ErrLoginQueueFull) {
		t.Fatalf("over-capacity acquire error = %v, want ErrLoginQueueFull", err)
	}
	assertLoginGauges(t, 1, 1)
	if got := loginCounter("queue_full") - counterBefore; got != 1 {
		t.Fatalf("login_limit_total{result=queue_full} delta = %v, want 1", got)
	}
	if got := loginHistogramCount(t, "queue_full") - histogramBefore; got != 1 {
		t.Fatalf("login_wait_duration_seconds{result=queue_full} count delta = %d, want 1", got)
	}

	first.Release()
	queued := <-queuedResult
	if queued.err != nil {
		t.Fatalf("queued acquire: %v", queued.err)
	}
	queued.permit.Release()
	assertLoginGauges(t, 0, 0)
}

func TestLoginLimiterQueueTimeoutRemovesWaiter(t *testing.T) {
	resetLoginGauges(t)
	bucket := &scriptedBucket{results: []bool{true, true}}
	registered := make(chan *fakeLoginTimer, 1)
	timerFactory := func(time.Duration) loginTimer {
		timer := newFakeLoginTimer()
		registered <- timer
		return timer
	}
	counterBefore := loginCounter("queue_timeout")
	histogramBefore := loginHistogramCount(t, "queue_timeout")
	limiter := newLoginLimiter(validLimiterConfig(), bucket, timerFactory, fixedLoginNow)

	first, err := limiter.acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	result := make(chan loginAcquireResult, 1)
	go func() {
		permit, acquireErr := limiter.acquire(context.Background())
		result <- loginAcquireResult{permit: permit, err: acquireErr}
	}()
	timer := <-registered
	assertLoginGauges(t, 1, 1)
	timer.ch <- fixedLoginNow()

	got := <-result
	if got.permit != nil {
		t.Fatal("timed-out acquire returned a permit")
	}
	if !errors.Is(got.err, ErrLoginQueueTimeout) {
		t.Fatalf("timed-out acquire error = %v, want ErrLoginQueueTimeout", got.err)
	}
	if !timer.stopped.Load() {
		t.Fatal("timed-out acquire timer was not stopped")
	}
	assertLoginGauges(t, 1, 0)
	if got := loginCounter("queue_timeout") - counterBefore; got != 1 {
		t.Fatalf("login_limit_total{result=queue_timeout} delta = %v, want 1", got)
	}
	if got := loginHistogramCount(t, "queue_timeout") - histogramBefore; got != 1 {
		t.Fatalf("login_wait_duration_seconds{result=queue_timeout} count delta = %d, want 1", got)
	}
	first.Release()
	assertLoginGauges(t, 0, 0)
}

func TestLoginLimiterContextCancellationRemovesWaiter(t *testing.T) {
	resetLoginGauges(t)
	bucket := &scriptedBucket{results: []bool{true, true}}
	registered := make(chan *fakeLoginTimer, 1)
	timerFactory := func(time.Duration) loginTimer {
		timer := newFakeLoginTimer()
		registered <- timer
		return timer
	}
	counterBefore := loginCounter("error")
	histogramBefore := loginHistogramCount(t, "error")
	limiter := newLoginLimiter(validLimiterConfig(), bucket, timerFactory, fixedLoginNow)

	first, err := limiter.acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan loginAcquireResult, 1)
	go func() {
		permit, acquireErr := limiter.acquire(ctx)
		result <- loginAcquireResult{permit: permit, err: acquireErr}
	}()
	timer := <-registered
	assertLoginGauges(t, 1, 1)
	cancel()

	got := <-result
	if got.permit != nil {
		t.Fatal("canceled acquire returned a permit")
	}
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("canceled acquire error = %v, want context.Canceled", got.err)
	}
	if !timer.stopped.Load() {
		t.Fatal("canceled acquire timer was not stopped")
	}
	assertLoginGauges(t, 1, 0)
	if got := loginCounter("error") - counterBefore; got != 1 {
		t.Fatalf("login_limit_total{result=error} delta = %v, want 1", got)
	}
	if got := loginHistogramCount(t, "error") - histogramBefore; got != 1 {
		t.Fatalf("login_wait_duration_seconds{result=error} count delta = %d, want 1", got)
	}
	first.Release()
	assertLoginGauges(t, 0, 0)
}

func TestLoginLimiterConcurrentMaxInflight(t *testing.T) {
	resetLoginGauges(t)
	const (
		requestCount = 64
		maxInflight  = 4
	)
	config := validLimiterConfig()
	config.MaxInflight = maxInflight
	config.QueueSize = requestCount - maxInflight
	bucket := &scriptedBucket{results: make([]bool, requestCount)}
	for i := range bucket.results {
		bucket.results[i] = true
	}
	registered := make(chan *fakeLoginTimer, requestCount-maxInflight)
	timerFactory := func(time.Duration) loginTimer {
		timer := newFakeLoginTimer()
		registered <- timer
		return timer
	}
	counterBefore := loginCounter("ok")
	histogramBefore := loginHistogramCount(t, "ok")
	limiter := newLoginLimiter(config, bucket, timerFactory, fixedLoginNow)

	initial := make([]loginPermit, 0, maxInflight)
	for range maxInflight {
		permit, err := limiter.acquire(context.Background())
		if err != nil {
			t.Fatalf("initial acquire: %v", err)
		}
		initial = append(initial, permit)
	}
	var active atomic.Int64
	var maximum atomic.Int64
	active.Store(maxInflight)
	maximum.Store(maxInflight)

	errs := make(chan error, requestCount-maxInflight)
	var workers sync.WaitGroup
	workers.Add(requestCount - maxInflight)
	for range requestCount - maxInflight {
		go func() {
			defer workers.Done()
			permit, err := limiter.acquire(context.Background())
			if err != nil {
				errs <- err
				return
			}
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			active.Add(-1)
			permit.Release()
		}()
	}

	timers := make([]*fakeLoginTimer, 0, requestCount-maxInflight)
	for range requestCount - maxInflight {
		timers = append(timers, <-registered)
	}
	assertLoginGauges(t, maxInflight, requestCount-maxInflight)
	for _, permit := range initial {
		active.Add(-1)
		permit.Release()
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent acquire: %v", err)
	}

	if got := maximum.Load(); got != maxInflight {
		t.Fatalf("maximum active permits = %d, want %d", got, maxInflight)
	}
	if got := active.Load(); got != 0 {
		t.Fatalf("active permits = %d, want 0", got)
	}
	for i, timer := range timers {
		if !timer.stopped.Load() {
			t.Fatalf("timer %d was not stopped", i)
		}
	}
	assertLoginGauges(t, 0, 0)
	if got := loginCounter("ok") - counterBefore; got != requestCount {
		t.Fatalf("login_limit_total{result=ok} delta = %v, want %d", got, requestCount)
	}
	if got := loginHistogramCount(t, "ok") - histogramBefore; got != requestCount {
		t.Fatalf("login_wait_duration_seconds{result=ok} count delta = %d, want %d", got, requestCount)
	}
}

func TestLoginLimiterConstructorUsesProductionBucketWithoutRefund(t *testing.T) {
	resetLoginGauges(t)
	config := validLimiterConfig()
	config.Rate = 1e-9
	config.Burst = 1
	counterBefore := loginCounter("rate_limited")
	histogramBefore := loginHistogramCount(t, "rate_limited")

	limiter, err := NewLoginLimiter(config)
	if err != nil {
		t.Fatalf("NewLoginLimiter: %v", err)
	}
	if _, ok := limiter.bucket.(*gxylimit.Bucket); !ok {
		t.Fatalf("production bucket type = %T, want *gxylimit.Bucket", limiter.bucket)
	}
	permit, err := limiter.acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	permit.Release()
	permit, err = limiter.acquire(context.Background())
	if permit != nil {
		t.Fatal("second acquire returned a permit after releasing concurrency slot")
	}
	if !errors.Is(err, ErrLoginRateLimited) {
		t.Fatalf("second acquire error = %v, want ErrLoginRateLimited", err)
	}

	assertLoginGauges(t, 0, 0)
	if got := loginCounter("rate_limited") - counterBefore; got != 1 {
		t.Fatalf("login_limit_total{result=rate_limited} delta = %v, want 1", got)
	}
	if got := loginHistogramCount(t, "rate_limited") - histogramBefore; got != 0 {
		t.Fatalf("login_wait_duration_seconds{result=rate_limited} count delta = %d, want 0", got)
	}
}

func TestLoginLimiterPackageStateFailsClosed(t *testing.T) {
	resetLoginGauges(t)
	restore := swapLoginAcquirer(unconfiguredLoginAcquirer{})
	t.Cleanup(restore)

	permit, err := currentLoginAcquirer.acquire(context.Background())
	if permit != nil {
		t.Fatal("unconfigured package state returned a permit")
	}
	if !errors.Is(err, ErrLoginLimiterUnconfigured) {
		t.Fatalf("unconfigured package state error = %v, want ErrLoginLimiterUnconfigured", err)
	}

	config := validLimiterConfig()
	config.Enabled = false
	limiter := newLoginLimiter(config, nil, unexpectedLoginTimer, fixedLoginNow)
	SetLoginLimiter(limiter)
	permit, err = currentLoginAcquirer.acquire(context.Background())
	if err != nil {
		t.Fatalf("configured package state acquire: %v", err)
	}
	permit.Release()

	SetLoginLimiter(nil)
	permit, err = currentLoginAcquirer.acquire(context.Background())
	if permit != nil {
		t.Fatal("nil-reset package state returned a permit")
	}
	if !errors.Is(err, ErrLoginLimiterUnconfigured) {
		t.Fatalf("nil-reset package state error = %v, want ErrLoginLimiterUnconfigured", err)
	}
	assertLoginGauges(t, 0, 0)
}
