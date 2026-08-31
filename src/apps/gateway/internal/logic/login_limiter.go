package logic

import (
	"context"
	"sync"
	"time"

	"gserver/core/gxylimit"
	"gserver/core/gxymetrics"

	"github.com/cockroachdb/errors"
)

var (
	ErrLoginRateLimited         = errors.New("login rate limited")
	ErrLoginQueueFull           = errors.New("login queue full")
	ErrLoginQueueTimeout        = errors.New("login queue timeout")
	ErrLoginLimiterUnconfigured = errors.New("login limiter not configured")
)

type tokenBucket interface {
	Allow() bool
}

type loginTimer interface {
	Chan() <-chan time.Time
	Stop() bool
}

type loginTimerFactory func(time.Duration) loginTimer

type loginPermit interface {
	Release()
}

type loginAcquirer interface {
	acquire(context.Context) (loginPermit, error)
}

type LoginLimiter struct {
	enabled      bool
	bucket       tokenBucket
	slots        chan struct{}
	queueSize    int
	waitTimeout  time.Duration
	timerFactory loginTimerFactory
	now          func() time.Time

	mu     sync.Mutex
	queued int
}

type timerAdapter struct {
	timer *time.Timer
}

func (t timerAdapter) Chan() <-chan time.Time {
	return t.timer.C
}

func (t timerAdapter) Stop() bool {
	return t.timer.Stop()
}

type noopLoginPermit struct{}

func (noopLoginPermit) Release() {}

type slotLoginPermit struct {
	limiter *LoginLimiter
	once    sync.Once
}

func (p *slotLoginPermit) Release() {
	p.once.Do(func() {
		<-p.limiter.slots
		gxymetrics.LoginInflight.Dec()
	})
}

type unconfiguredLoginAcquirer struct{}

func (unconfiguredLoginAcquirer) acquire(context.Context) (loginPermit, error) {
	return nil, ErrLoginLimiterUnconfigured
}

var currentLoginAcquirer loginAcquirer = unconfiguredLoginAcquirer{}

func NewLoginLimiter(config LoginLimitConfig) (*LoginLimiter, error) {
	if err := validateLoginLimitConfig(config); err != nil {
		return nil, err
	}

	if !config.Enabled {
		return newLoginLimiter(config, nil, newProductionLoginTimer, time.Now), nil
	}
	bucket, err := gxylimit.NewBucket(gxylimit.Config{
		Rate:  config.Rate,
		Burst: config.Burst,
	})
	if err != nil {
		return nil, errors.Wrap(err, "create login token bucket")
	}
	return newLoginLimiter(config, bucket, newProductionLoginTimer, time.Now), nil
}

func newLoginLimiter(
	config LoginLimitConfig,
	bucket tokenBucket,
	timerFactory loginTimerFactory,
	now func() time.Time,
) *LoginLimiter {
	return &LoginLimiter{
		enabled:      config.Enabled,
		bucket:       bucket,
		slots:        make(chan struct{}, config.MaxInflight),
		queueSize:    config.QueueSize,
		waitTimeout:  config.WaitTimeout,
		timerFactory: timerFactory,
		now:          now,
	}
}

func newProductionLoginTimer(timeout time.Duration) loginTimer {
	return timerAdapter{timer: time.NewTimer(timeout)}
}

func SetLoginLimiter(limiter *LoginLimiter) {
	if limiter == nil {
		currentLoginAcquirer = unconfiguredLoginAcquirer{}
		return
	}
	currentLoginAcquirer = limiter
}

func swapLoginAcquirer(acquirer loginAcquirer) func() {
	previous := currentLoginAcquirer
	currentLoginAcquirer = acquirer
	return func() {
		currentLoginAcquirer = previous
	}
}

func (l *LoginLimiter) acquire(ctx context.Context) (loginPermit, error) {
	if !l.enabled {
		gxymetrics.LoginLimitTotal.WithLabelValues("ok").Inc()
		return noopLoginPermit{}, nil
	}
	if !l.bucket.Allow() {
		gxymetrics.LoginLimitTotal.WithLabelValues("rate_limited").Inc()
		return nil, ErrLoginRateLimited
	}

	started := l.now()
	select {
	case l.slots <- struct{}{}:
		return l.acquiredPermit(started), nil
	default:
	}

	if !l.enterQueue() {
		l.observeGateResult("queue_full", started)
		return nil, ErrLoginQueueFull
	}
	timer := l.timerFactory(l.waitTimeout)
	defer timer.Stop()

	// Go's select picks uniformly at random among ready cases, so a
	// concurrently canceled acquire may still win a freed slot instead of
	// observing ctx.Done(). That is benign and matches the approved design:
	// the caller keeps a valid permit and the queue is left exactly once.
	select {
	case l.slots <- struct{}{}:
		l.leaveQueue()
		return l.acquiredPermit(started), nil
	case <-timer.Chan():
		l.leaveQueue()
		l.observeGateResult("queue_timeout", started)
		return nil, ErrLoginQueueTimeout
	case <-ctx.Done():
		l.leaveQueue()
		l.observeGateResult("error", started)
		return nil, errors.Wrap(ctx.Err(), "wait for login permit")
	}
}

func (l *LoginLimiter) enterQueue() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.queued >= l.queueSize {
		return false
	}
	l.queued++
	gxymetrics.LoginQueueLength.Set(float64(l.queued))
	return true
}

func (l *LoginLimiter) leaveQueue() {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Guard against a future state-machine regression that could call
	// leaveQueue without a matching enterQueue: decrementing below zero
	// would leak a negative queue gauge, so underflow becomes a no-op.
	if l.queued > 0 {
		l.queued--
		gxymetrics.LoginQueueLength.Set(float64(l.queued))
	}
}

func (l *LoginLimiter) acquiredPermit(started time.Time) loginPermit {
	gxymetrics.LoginInflight.Inc()
	gxymetrics.LoginLimitTotal.WithLabelValues("ok").Inc()
	gxymetrics.LoginWaitDuration.WithLabelValues("ok").Observe(l.now().Sub(started).Seconds())
	return &slotLoginPermit{limiter: l}
}

func (l *LoginLimiter) observeGateResult(result string, started time.Time) {
	gxymetrics.LoginLimitTotal.WithLabelValues(result).Inc()
	gxymetrics.LoginWaitDuration.WithLabelValues(result).Observe(l.now().Sub(started).Seconds())
}
