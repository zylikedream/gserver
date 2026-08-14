// Package errortest 验证错误处理规范(cockroachdb/errors)的硬性规则。
//
// 规则来源: docs/development/error-handling.md
// 本测试用实验事实固化规范, 未来有人违反规范(如用 fmt.Errorf %w 包装
// cockroachdb 错误、对已有栈的错误 WithStack), 此测试即失败。
//
// 栈段数统计: cockroachdb %+v 输出中每段栈出现 2 次 "stack trace" 标记
// (标题 "attached stack trace" + 分隔线 "-- stack trace:"), 故除以 2。
package errortest

import (
	"errors" // 标准库(哨兵)
	"fmt"
	"strings"
	"testing"

	cerrors "github.com/cockroachdb/errors"
)

// ---------- 哨兵 ----------

var (
	stdSentinel  = errors.New("std sentinel")        // 标准库哨兵(无栈)
	cerrSentinel = cerrors.New("cockroach sentinel") // cockroachdb 哨兵(定义处栈)
)

// ---------- 产生点 ----------

func produceStdErr() error { return errors.New("plain std error") }       // 标准库临时错误(无栈)
func produceCerr() error   { return cerrors.New("cockroach temp error") } // cockroachdb 临时错误(产生点栈, 本行)

// ---------- 辅助 ----------

func stackCount(err error) int {
	return strings.Count(fmt.Sprintf("%+v", err), "stack trace") / 2
}

func containsLine(err error, line int) bool {
	return strings.Contains(fmt.Sprintf("%+v", err), fmt.Sprintf("error_semantics_test.go:%d", line))
}

// ---------- 测试 ----------

// TestStackCount 固化各组合的栈段数(规范硬性规则)。
func TestStackCount(t *testing.T) {
	type tc struct {
		name string
		err  func() error
		want int
	}
	cases := []tc{
		// 基础: 标准库无栈, cockroachdb 带栈
		{"标准库临时错误(裸)", func() error { return produceStdErr() }, 0},
		{"cockroachdb 临时错误(裸)", func() error { return produceCerr() }, 1},
		{"标准库哨兵(裸)", func() error { return stdSentinel }, 0},
		{"cockroachdb 哨兵(裸)", func() error { return cerrSentinel }, 1},

		// Wrap: 每次 Wrap 加 1 段独立栈(规范允许, 信息递增)
		{"Wrap(标准库临时)", func() error { return cerrors.Wrap(produceStdErr(), "ctx") }, 1},
		{"Wrap(cockroachdb 临时)", func() error { return cerrors.Wrap(produceCerr(), "ctx") }, 2},
		{"Wrap(标准库哨兵)", func() error { return cerrors.Wrap(stdSentinel, "ctx") }, 1},
		{"Wrap(cockroachdb 哨兵)", func() error { return cerrors.Wrap(cerrSentinel, "ctx") }, 2},
		{"New→Wrap→Wrap", func() error { return cerrors.Wrap(cerrors.Wrap(produceCerr(), "L1"), "L2") }, 3},

		// WithStack: 对已有栈的错误 = 重复(规范禁止); 对无栈错误 = 补栈(规范允许)
		{"WithStack(标准库临时)", func() error { return cerrors.WithStack(produceStdErr()) }, 1},
		{"WithStack(cockroachdb 临时)", func() error { return cerrors.WithStack(produceCerr()) }, 2}, // 重复
		{"WithStack(标准库哨兵)", func() error { return cerrors.WithStack(stdSentinel) }, 1},
		{"WithStack(cockroachdb 哨兵)", func() error { return cerrors.WithStack(cerrSentinel) }, 2}, // 重复

		// 链式
		{"哨兵→WithStack→Wrap", func() error { return cerrors.Wrap(cerrors.WithStack(stdSentinel), "L1") }, 2},

		// 标准库 fmt.Errorf %w = 栈杀手(规范禁止包装 cockroachdb 错误)
		{"fmt.Errorf %w(cockroachdb 临时)", func() error { return fmt.Errorf("wrap %w", produceCerr()) }, 0},
		{"fmt.Errorf %w(标准库哨兵)", func() error { return fmt.Errorf("wrap %w", stdSentinel) }, 0},
	}
	for _, c := range cases {
		if got := stackCount(c.err()); got != c.want {
			t.Errorf("%s: 栈段=%d, 期望 %d", c.name, got, c.want)
		}
	}
}

// TestIsCompatibility 固化 errors.Is 匹配(所有组合必须穿透)。
func TestIsCompatibility(t *testing.T) {
	if !errors.Is(stdSentinel, stdSentinel) {
		t.Error("标准库哨兵 Is 自身失败")
	}
	if !errors.Is(cerrors.Wrap(stdSentinel, "ctx"), stdSentinel) {
		t.Error("Wrap(标准库哨兵) Is 穿透失败")
	}
	if !errors.Is(cerrors.WithStack(stdSentinel), stdSentinel) {
		t.Error("WithStack(标准库哨兵) Is 穿透失败")
	}
	if !errors.Is(fmt.Errorf("wrap %w", stdSentinel), stdSentinel) {
		t.Error("fmt.Errorf %w(标准库哨兵) Is 穿透失败")
	}
	if !errors.Is(cerrors.Wrap(cerrSentinel, "ctx"), cerrSentinel) {
		t.Error("Wrap(cockroachdb 哨兵) Is 穿透失败")
	}
}

// TestLocateProducePoint 固化定位能力: 栈必须包含实际产生点行号。
func TestLocateProducePoint(t *testing.T) {
	// cockroachdb 临时错误: 栈必须包含产生点函数名(可定位)
	if !strings.Contains(fmt.Sprintf("%+v", produceCerr()), "produceCerr") {
		t.Error("cockroachdb 临时错误栈未包含产生点函数")
	}

	// 标准库哨兵 + WithStack: 栈含 WithStack 调用处(抛错位置), 而非哨兵定义处
	wsErr := cerrors.WithStack(stdSentinel)
	stack := fmt.Sprintf("%+v", wsErr)
	if !strings.Contains(stack, "TestLocateProducePoint") {
		t.Error("WithStack(哨兵) 栈未包含抛错位置")
	}
	// 标准库哨兵本身无栈, 定义处不应出现 "stack trace"
	if strings.Contains(fmt.Sprintf("%+v", stdSentinel), "stack trace") {
		t.Error("标准库哨兵不应有栈")
	}
}
