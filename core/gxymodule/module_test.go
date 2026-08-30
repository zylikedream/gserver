package gxymodule

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// plainMod 不覆盖任何方法:用于验证 ModuleBase 默认命名与默认空实现。
type plainMod struct {
	ModuleBase
}

// seqMod 覆盖 GetModName 与全部生命周期钩子,记录调用顺序,可注入阶段错误。
type seqMod struct {
	ModuleBase
	id    string
	log   *[]string
	errOn string
	err   error
}

func (m *seqMod) GetModName() string { return m.id }

func (m *seqMod) hook(phase string, ctx context.Context) error {
	*m.log = append(*m.log, m.id+":"+phase)
	if m.errOn == phase {
		return m.err
	}
	return nil
}

func (m *seqMod) OnModInit(ctx context.Context) error        { return m.hook("init", ctx) }
func (m *seqMod) OnModStart(ctx context.Context) error       { return m.hook("start", ctx) }
func (m *seqMod) OnModStartAfter(ctx context.Context) error  { return m.hook("startAfter", ctx) }
func (m *seqMod) OnModStopBefore(ctx context.Context) error  { return m.hook("stopBefore", ctx) }
func (m *seqMod) OnModStop(ctx context.Context) error        { return m.hook("stop", ctx) }

func TestModuleBaseDefaultHooksReturnNil(t *testing.T) {
	m := &plainMod{}
	ctx := context.Background()
	if err := m.OnModInit(ctx); err != nil {
		t.Fatalf("OnModInit = %v, want nil", err)
	}
	if err := m.OnModStart(ctx); err != nil {
		t.Fatalf("OnModStart = %v, want nil", err)
	}
	if err := m.OnModStartAfter(ctx); err != nil {
		t.Fatalf("OnModStartAfter = %v, want nil", err)
	}
	if err := m.OnModStop(ctx); err != nil {
		t.Fatalf("OnModStop = %v, want nil", err)
	}
	if err := m.OnModStopBefore(ctx); err != nil {
		t.Fatalf("OnModStopBefore = %v, want nil", err)
	}
}

func TestAddModuleSetsIdentity(t *testing.T) {
	root := &plainMod{}
	root.SetSelfMod(root)
	child := &plainMod{}

	if err := root.AddModule(context.Background(), child); err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	// 默认命名来自反射类型名(去指针)。
	if got := child.GetModName(); got != "plainMod" {
		t.Errorf("GetModName = %q, want %q", got, "plainMod")
	}
	// self/parent 绑定。
	if child.BaseModule().self != child {
		t.Error("self not bound to child")
	}
	if child.BaseModule().parent != root {
		t.Error("parent not bound to root")
	}
	// 注册进父模块 childs。
	if got := root.Modules(); len(got) != 1 || got[0] != child {
		t.Errorf("Modules() = %v, want [child]", got)
	}
	if root.GetModule("plainMod") != child {
		t.Error("GetModule(plainMod) did not return child")
	}
	if root.GetModule("missing") != nil {
		t.Error("GetModule(missing) = non-nil, want nil")
	}
	if child.GetParent() != root {
		t.Error("GetParent() != root")
	}
}

func TestAddModuleInitErrorSkipsChild(t *testing.T) {
	root := &plainMod{}
	root.SetSelfMod(root)
	var badLog []string
	bad := &seqMod{id: "bad", log: &badLog, errOn: "init", err: errors.New("init failed")}

	if err := root.AddModule(context.Background(), bad); err == nil {
		t.Fatal("AddModule = nil, want init error")
	}
	if got := root.Modules(); len(got) != 0 {
		t.Errorf("Modules() = %v, want empty (failed child must not register)", got)
	}
	if root.GetModule("bad") != nil {
		t.Error("failed child still findable via GetModule")
	}
}

func TestStartModuleOrder(t *testing.T) {
	root := &plainMod{}
	root.SetSelfMod(root)
	var log []string
	a := &seqMod{id: "A", log: &log}
	b := &seqMod{id: "B", log: &log}
	ctx := context.Background()
	if err := root.AddModule(ctx, a); err != nil {
		t.Fatalf("AddModule A: %v", err)
	}
	if err := root.AddModule(ctx, b); err != nil {
		t.Fatalf("AddModule B: %v", err)
	}

	if err := root.StartModule(ctx); err != nil {
		t.Fatalf("StartModule: %v", err)
	}

	// 深度优先 start,再统一 startAfter(父节点无日志:plainMod 不记录)。
	want := []string{"A:init", "B:init", "A:start", "B:start", "A:startAfter", "B:startAfter"}
	if !reflect.DeepEqual(log, want) {
		t.Errorf("start order = %v, want %v", log, want)
	}
}

func TestStartModuleErrorStopsPropagation(t *testing.T) {
	root := &plainMod{}
	root.SetSelfMod(root)
	var log []string
	a := &seqMod{id: "A", log: &log, errOn: "start", err: errors.New("boom")}
	b := &seqMod{id: "B", log: &log}
	ctx := context.Background()
	if err := root.AddModule(ctx, a); err != nil {
		t.Fatalf("AddModule A: %v", err)
	}
	if err := root.AddModule(ctx, b); err != nil {
		t.Fatalf("AddModule B: %v", err)
	}

	if err := root.StartModule(ctx); err == nil {
		t.Fatal("StartModule = nil, want child start error")
	}
	// A 失败后 B 不得启动;startAfter 也不得执行。
	if len(log) != 3 || log[0] != "A:init" || log[1] != "B:init" || log[2] != "A:start" {
		t.Errorf("log = %v, want [A:start] only", log)
	}
	for _, phase := range log {
		if phase == "B:start" || phase == "B:startAfter" || phase == "A:startAfter" {
			t.Errorf("unexpected phase %q after start failure", phase)
		}
	}
}

func TestStartAfterErrorPropagates(t *testing.T) {
	root := &plainMod{}
	root.SetSelfMod(root)
	var log []string
	a := &seqMod{id: "A", log: &log}
	b := &seqMod{id: "B", log: &log, errOn: "startAfter", err: errors.New("after boom")}
	ctx := context.Background()
	_ = root.AddModule(ctx, a)
	_ = root.AddModule(ctx, b)

	if err := root.StartModule(ctx); err == nil {
		t.Fatal("StartModule = nil, want startAfter error")
	}
}

func TestStopModuleReverseOrderAndCleanup(t *testing.T) {
	root := &plainMod{}
	root.SetSelfMod(root)
	var log []string
	a := &seqMod{id: "A", log: &log}
	b := &seqMod{id: "B", log: &log}
	ctx := context.Background()
	_ = root.AddModule(ctx, a)
	_ = root.AddModule(ctx, b)
	if err := root.StartModule(ctx); err != nil {
		t.Fatalf("StartModule: %v", err)
	}
	log = nil

	if err := root.StopModule(ctx); err != nil {
		t.Fatalf("StopModule: %v", err)
	}
	// 逆序:先停子节点,再停自身。
	want := []string{"B:stopBefore", "B:stop", "A:stopBefore", "A:stop", "root:stopBefore", "root:stop"}
	// root 是 plainMod,不记录 stop;断言子节点部分与 root 状态清理。
	if len(log) != 4 || !reflect.DeepEqual(log, want[:4]) {
		t.Errorf("stop order = %v, want %v", log, want[:4])
	}
	if root.BaseModule().self != nil || root.BaseModule().parent != nil || root.BaseModule().childs != nil {
		t.Error("root state not cleared after StopModule")
	}
	if child := a.BaseModule(); child.self != nil || child.childs != nil {
		t.Error("child state not cleared after StopModule")
	}
}

func TestStopModuleReturnsError(t *testing.T) {
	root := &seqMod{id: "root", log: &[]string{}, errOn: "stop", err: errors.New("root stop failed")}
	root.SetSelfMod(root)
	a := &seqMod{id: "A", log: &[]string{}}
	ctx := context.Background()
	_ = root.AddModule(ctx, a)
	_ = root.StartModule(ctx)

	if err := root.StopModule(ctx); err == nil {
		t.Fatal("StopModule = nil, want child stop error")
	}
}

func TestStartStopEmptyModule(t *testing.T) {
	root := &plainMod{}
	root.SetSelfMod(root)
	ctx := context.Background()
	if err := root.StartModule(ctx); err != nil {
		t.Fatalf("StartModule on empty: %v", err)
	}
	if err := root.StopModule(ctx); err != nil {
		t.Fatalf("StopModule on empty: %v", err)
	}
}
