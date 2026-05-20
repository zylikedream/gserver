package logic

import (
	"context"
	"reflect"
	"testing"
)

func TestWithPlotLocksSortsAndReleases(t *testing.T) {
	oldLocks := plotLocks
	mem := newMemoryPlotLockManager()
	plotLocks = mem
	t.Cleanup(func() { plotLocks = oldLocks })

	err := withPlotLocks(context.Background(), 1001, []int32{3, 1, 2, 1}, func() error {
		if len(mem.held) != 3 {
			t.Fatalf("expected 3 held locks, got %d", len(mem.held))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{
		plotLockKey(1001, 1),
		plotLockKey(1001, 2),
		plotLockKey(1001, 3),
	}
	if !reflect.DeepEqual(mem.order, wantOrder) {
		t.Fatalf("unexpected lock order: got %v want %v", mem.order, wantOrder)
	}
	if len(mem.held) != 0 {
		t.Fatalf("expected locks released, still held: %v", mem.held)
	}
}

func TestWithPlotLocksReturnsBusyAndReleasesPartial(t *testing.T) {
	oldLocks := plotLocks
	mem := newMemoryPlotLockManager()
	mem.blocked[plotLockKey(1001, 2)] = true
	plotLocks = mem
	t.Cleanup(func() { plotLocks = oldLocks })

	err := withPlotLocks(context.Background(), 1001, []int32{1, 2}, func() error {
		t.Fatal("callback should not run")
		return nil
	})
	if err != ErrPlotBusy {
		t.Fatalf("expected ErrPlotBusy, got %v", err)
	}
	if len(mem.held) != 0 {
		t.Fatalf("expected partial lock released, still held: %v", mem.held)
	}
}
