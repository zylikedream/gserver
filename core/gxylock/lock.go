package gxylock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	mathrand "math/rand"
	"sort"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/redis/go-redis/v9"
)

const (
	defaultAcquireAttempts = 3
	defaultRetryBaseDelay  = 30 * time.Millisecond
)

var ErrBusy = errors.New("lock busy")

type Manager interface {
	Acquire(ctx context.Context, key string, ttl time.Duration) (string, bool, error)
	Release(ctx context.Context, key string, token string)
}

type RedisManager struct {
	ClientProvider  func() redis.UniversalClient
	AcquireAttempts int
	RetryBaseDelay  time.Duration
}

func NewRedisManager(provider func() redis.UniversalClient) *RedisManager {
	return &RedisManager{
		ClientProvider:  provider,
		AcquireAttempts: defaultAcquireAttempts,
		RetryBaseDelay:  defaultRetryBaseDelay,
	}
}

func (m *RedisManager) Acquire(ctx context.Context, key string, ttl time.Duration) (string, bool, error) {
	client := m.client()
	if client == nil {
		return "", false, errors.New("redis lock client not initialized")
	}

	token, err := newToken()
	if err != nil {
		return "", false, err
	}

	attempts := m.acquireAttempts()
	for attempt := 0; attempt < attempts; attempt++ {
		ok, err := client.SetNX(ctx, key, token, ttl).Result()
		if err != nil {
			return "", false, err
		}
		if ok {
			return token, true, nil
		}
		if attempt == attempts-1 {
			break
		}
		if err := sleepBeforeRetry(ctx, m.retryBaseDelay(), attempt); err != nil {
			return "", false, err
		}
	}
	return "", false, nil
}

func (m *RedisManager) Release(ctx context.Context, key string, token string) {
	client := m.client()
	if client == nil {
		return
	}
	_ = releaseScript.Run(ctx, client, []string{key}, token).Err()
}

func (m *RedisManager) client() redis.UniversalClient {
	if m == nil || m.ClientProvider == nil {
		return nil
	}
	return m.ClientProvider()
}

func (m *RedisManager) acquireAttempts() int {
	if m == nil || m.AcquireAttempts <= 0 {
		return defaultAcquireAttempts
	}
	return m.AcquireAttempts
}

func (m *RedisManager) retryBaseDelay() time.Duration {
	if m == nil || m.RetryBaseDelay <= 0 {
		return defaultRetryBaseDelay
	}
	return m.RetryBaseDelay
}

func With(ctx context.Context, mgr Manager, keys []string, ttl time.Duration, fn func() error) error {
	if fn == nil {
		return nil
	}
	keys = uniqueSortedStrings(keys)
	if len(keys) == 0 {
		return fn()
	}

	held := make([]struct {
		key   string
		token string
	}, 0, len(keys))
	for _, key := range keys {
		token, ok, err := mgr.Acquire(ctx, key, ttl)
		if err != nil {
			releaseHeld(ctx, mgr, held)
			return err
		}
		if !ok {
			releaseHeld(ctx, mgr, held)
			return ErrBusy
		}
		held = append(held, struct {
			key   string
			token string
		}{key: key, token: token})
	}
	defer releaseHeld(ctx, mgr, held)
	return fn()
}

var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

func releaseHeld(ctx context.Context, mgr Manager, held []struct {
	key   string
	token string
}) {
	for i := len(held) - 1; i >= 0; i-- {
		mgr.Release(ctx, held[i].key, held[i].token)
	}
}

func newToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func sleepBeforeRetry(ctx context.Context, baseDelay time.Duration, attempt int) error {
	if baseDelay <= 0 {
		return nil
	}
	delay := baseDelay * time.Duration(1<<attempt)
	jitter := time.Duration(mathrand.Int63n(int64(baseDelay)))
	timer := time.NewTimer(delay + jitter)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
