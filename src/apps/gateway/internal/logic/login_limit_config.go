package logic

import (
	"context"
	"math"
	"time"

	"gserver/core/gxyutil"

	"github.com/cockroachdb/errors"
	"github.com/gogf/gf/v2/os/gcfg"
)

type LoginLimitConfig struct {
	Enabled     bool
	Rate        float64
	Burst       int
	MaxInflight int
	QueueSize   int
	WaitTimeout time.Duration
}

type rawLoginLimitConfig struct {
	Enabled     *bool          `toml:"enabled" mapstructure:"enabled"`
	Rate        *float64       `toml:"rate" mapstructure:"rate"`
	Burst       *int           `toml:"burst" mapstructure:"burst"`
	MaxInflight *int           `toml:"max_inflight" mapstructure:"max_inflight"`
	QueueSize   *int           `toml:"queue_size" mapstructure:"queue_size"`
	WaitTimeout *time.Duration `toml:"wait_timeout" mapstructure:"wait_timeout"`
}

func LoadLoginLimitConfig(ctx context.Context, cfg *gcfg.Config) (LoginLimitConfig, error) {
	var raw rawLoginLimitConfig
	if err := gxyutil.CfgUnmarshalKeyExact(ctx, cfg, "login_limit", &raw); err != nil {
		return LoginLimitConfig{}, errors.Wrap(err, "decode login_limit")
	}
	if raw.Enabled == nil || raw.Rate == nil || raw.Burst == nil || raw.MaxInflight == nil || raw.QueueSize == nil || raw.WaitTimeout == nil {
		return LoginLimitConfig{}, errors.New("login_limit.enabled, login_limit.rate, login_limit.burst, login_limit.max_inflight, login_limit.queue_size, and login_limit.wait_timeout are required")
	}
	config := LoginLimitConfig{
		Enabled:     *raw.Enabled,
		Rate:        *raw.Rate,
		Burst:       *raw.Burst,
		MaxInflight: *raw.MaxInflight,
		QueueSize:   *raw.QueueSize,
		WaitTimeout: *raw.WaitTimeout,
	}
	if err := validateLoginLimitConfig(config); err != nil {
		return LoginLimitConfig{}, err
	}
	return config, nil
}

func validateLoginLimitConfig(config LoginLimitConfig) error {
	if math.IsNaN(config.Rate) || math.IsInf(config.Rate, 0) || config.Rate <= 0 {
		return errors.Newf("invalid login_limit.rate %v: must be a positive finite number", config.Rate)
	}
	if config.Burst <= 0 {
		return errors.Newf("invalid login_limit.burst %d: must be a positive integer", config.Burst)
	}
	if config.MaxInflight <= 0 {
		return errors.Newf("invalid login_limit.max_inflight %d: must be a positive integer", config.MaxInflight)
	}
	if config.QueueSize < 0 {
		return errors.Newf("invalid login_limit.queue_size %d: must be non-negative", config.QueueSize)
	}
	if config.WaitTimeout <= 0 {
		return errors.Newf("invalid login_limit.wait_timeout %s: must be positive", config.WaitTimeout)
	}
	return nil
}
