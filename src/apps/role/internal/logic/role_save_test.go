package logic

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gserver/core/gxymodule"
)

type testPersistState struct {
	RolePersistState
	table string
}

func (s *testPersistState) TableName() string { return s.table }

type testRoleModule struct {
	gxymodule.ModuleBase
	state *testPersistState
}

func (m *testRoleModule) SetRole(role *RoleMain)         {}
func (m *testRoleModule) OnCreate(ctx context.Context)   {}
func (m *testRoleModule) AfterLogin(ctx context.Context) {}
func (m *testRoleModule) BeforeLogout(ctx context.Context) {
}
func (m *testRoleModule) PersistState() IPersistState {
	if m.state == nil {
		return nil
	}
	return m.state
}

func TestDirtyRoleModulesOnlyReturnsDirtyPersistModules(t *testing.T) {
	ctx := context.Background()
	r := NewRoleMain()
	dirty := &testRoleModule{state: &testPersistState{table: "dirty_mod"}}
	clean := &testRoleModule{state: &testPersistState{table: "clean_mod"}}
	withoutState := &testRoleModule{}
	dirty.state.MarkDirty()

	if err := r.AddModule(ctx, dirty); err != nil {
		t.Fatal(err)
	}
	if err := r.AddModule(ctx, clean); err != nil {
		t.Fatal(err)
	}
	if err := r.AddModule(ctx, withoutState); err != nil {
		t.Fatal(err)
	}

	mods := r.dirtyRoleModules()
	if len(mods) != 1 {
		t.Fatalf("dirtyRoleModules len = %d, want 1", len(mods))
	}
	if mods[0] != dirty {
		t.Fatalf("dirtyRoleModules[0] = %T, want dirty module", mods[0])
	}
}

func TestRoleSaveLimiterCapsConcurrentSaves(t *testing.T) {
	limiter := newRoleSaveLimiter(2)

	var active int32
	var maxActive int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, err := limiter.acquire(context.Background())
			if err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}
			cur := atomic.AddInt32(&active, 1)
			for {
				prev := atomic.LoadInt32(&maxActive)
				if cur <= prev || atomic.CompareAndSwapInt32(&maxActive, prev, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			release()
		}()
	}

	close(start)
	wg.Wait()

	if maxActive > 2 {
		t.Fatalf("max active saves = %d, want <= 2", maxActive)
	}
}
