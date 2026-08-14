package gxylock

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
)

type memoryManager struct {
	held    map[string]string
	order   []string
	blocked map[string]bool
}

func newMemoryManager() *memoryManager {
	return &memoryManager{held: make(map[string]string), blocked: make(map[string]bool)}
}

func (m *memoryManager) Acquire(_ context.Context, key string, _ time.Duration) (string, bool, error) {
	m.order = append(m.order, key)
	if m.blocked[key] {
		return "", false, nil
	}
	if _, ok := m.held[key]; ok {
		return "", false, nil
	}
	token := key + ":token"
	m.held[key] = token
	return token, true, nil
}

func (m *memoryManager) Release(_ context.Context, key string, token string) {
	if m.held[key] == token {
		delete(m.held, key)
	}
}

func TestWithSortsAndReleases(t *testing.T) {
	mem := newMemoryManager()
	err := With(context.Background(), mem, []string{"3", "1", "2", "1"}, time.Second, func() error {
		if len(mem.held) != 3 {
			t.Fatalf("expected 3 held locks, got %d", len(mem.held))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"1", "2", "3"}
	if !reflect.DeepEqual(mem.order, wantOrder) {
		t.Fatalf("unexpected lock order: got %v want %v", mem.order, wantOrder)
	}
	if len(mem.held) != 0 {
		t.Fatalf("expected locks released, still held: %v", mem.held)
	}
}

func TestWithReturnsBusyAndReleasesPartial(t *testing.T) {
	mem := newMemoryManager()
	mem.blocked["2"] = true
	err := With(context.Background(), mem, []string{"1", "2"}, time.Second, func() error {
		t.Fatal("callback should not run")
		return nil
	})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
	if len(mem.held) != 0 {
		t.Fatalf("expected partial lock released, still held: %v", mem.held)
	}
}

func TestSleepBeforeRetryHonorsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := sleepBeforeRetry(ctx, 30*time.Millisecond, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if time.Since(start) > 20*time.Millisecond {
		t.Fatalf("expected immediate cancel, took %s", time.Since(start))
	}
}
