package gxyactor

import (
	"testing"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

type kindProbe struct{ *Actor }

// 工厂必须把注册名的键写入实例:构造处写的能力名会被覆写为它。
//
// 这条曾经名存实亡——实现里用的是类型断言 behavior.(*Actor),而业务对象是
// 内嵌基类的外层结构体(不是 *Actor),断言永不成立,覆写从未发生。接口断言
// 才查得到提升的方法。
func TestAsFactoryOverridesKind(t *testing.T) {
	b := &kindProbe{Actor: NewActor("WRONG")}
	if _, err := unit.Spawn(t, asFactory("guild", func() act.ActorBehavior { return b }), gen.ProcessOptions{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if b.kind != "guild" {
		t.Fatalf("kind = %q, want 框架覆写为 guild", b.kind)
	}
}
