package gxylimit

import (
	"math"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
)

type Config struct {
	Rate  float64
	Burst int
}

type Bucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
	now    func() time.Time
}

func NewBucket(config Config) (*Bucket, error) {
	return newBucket(config, time.Now)
}

func newBucket(config Config, now func() time.Time) (*Bucket, error) {
	if config.Rate <= 0 || math.IsNaN(config.Rate) || math.IsInf(config.Rate, 0) {
		return nil, errors.New("rate must be finite and positive")
	}
	if config.Burst <= 0 {
		return nil, errors.New("burst must be positive")
	}

	return &Bucket{
		rate:   config.Rate,
		burst:  float64(config.Burst),
		tokens: float64(config.Burst),
		last:   now(),
		now:    now,
	}, nil
}

func (b *Bucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = math.Min(b.burst, b.tokens+elapsed*b.rate)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
