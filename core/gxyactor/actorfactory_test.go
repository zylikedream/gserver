package gxyactor

import (
	"testing"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

// kindProbe 是最小业务对象:只内嵌基类,不实现任何业务入口。
type kindProbe struct{ *Actor }

// 工厂必须把注册名的键写入实例,业务构造器不参与命名。
//
// 这条曾经名存实亡——旧实现用类型断言 behavior.(*Actor) 写入,
// 而业务对象是内嵌基类的外层结构体(不是 *Actor),断言永不成立。接口断言
// 才查得到提升的方法。
func TestActorFactoryWritesRegistryKeyAsKind(t *testing.T) {
	var probe *kindProbe
	factory := ActorFactory("guild", func() Business {
		probe = &kindProbe{Actor: NewActor()}
		return probe
	})
	if _, err := unit.Spawn(t, factory, gen.ProcessOptions{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if probe.kind != "guild" {
		t.Fatalf("kind = %q, want 注册表的键 guild", probe.kind)
	}
}

// 交给运行时的是适配对象,不是业务对象;业务对象藏在它里面。
//
// 这是隔离的实质:业务对象不满足运行时的行为契约,因此不可能被直接登记
// (见 ADR 0017)。
func TestRuntimeSeesAdapterNotBusiness(t *testing.T) {
	var probe *kindProbe
	factory := ActorFactory("guild", func() Business {
		probe = &kindProbe{Actor: NewActor()}
		return probe
	})
	subj, err := unit.Spawn(t, factory, gen.ProcessOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	rt, ok := subj.Behavior().(*runtimeActor)
	if !ok {
		t.Fatalf("运行时的行为实例类型 = %T, want *runtimeActor", subj.Behavior())
	}
	if rt.biz != Business(probe) {
		t.Fatal("适配对象必须持有业务对象")
	}
}
