package gxylimit

import (
	"math"
	"time"

	"golang.org/x/time/rate"

	"github.com/cockroachdb/errors"
)

type Config struct {
	Rate  float64
	Burst int
}

// Bucket 是限流桶:补充数学由 golang.org/x/time/rate 提供,此处只保留
// 本仓库的契约——构造校验与包内时钟注入(测试用假时钟,不 sleep)。
type Bucket struct {
	limiter *rate.Limiter
	now     func() time.Time
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
		limiter: rate.NewLimiter(rate.Limit(config.Rate), config.Burst),
		now:     now,
	}, nil
}

// Allow 报告此刻是否放行并消费一个令牌。时钟由构造注入,被拒时不推进库状态。
func (b *Bucket) Allow() bool {
	return b.limiter.AllowN(b.now(), 1)
}
