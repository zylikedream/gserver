package gxyactor

import (
	"testing"
	"time"

	"ergo.services/ergo"
	"ergo.services/ergo/gen"
)

// 激活协调者崩了必须被重建。
//
// 这是本节点跨节点激活的单点:节点只会拉起进程、不会重建崩溃的进程,所以没有托管
// 时它一崩,本节点的跨节点激活就失效到节点重启(见 ADR 0019)。
//
// 用真实节点而非 mock:重启行为由运行时的监督者机制提供,只有真实运行时才有。
func TestActivatorRouterIsRestartedAfterCrash(t *testing.T) {
	node, err := ergo.StartNode("suptest@localhost", gen.NodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { node.Stop() })

	spec := activatorSupervisorSpec(&activatorManager{})
	if _, err := node.Spawn(func() gen.ProcessBehavior {
		return &activatorSupervisor{spec: spec}
	}, gen.ProcessOptions{}); err != nil {
		t.Fatalf("spawn supervisor: %v", err)
	}

	// 协调者按名注册——跨节点寻址依赖这个名字。
	first, err := node.ProcessPID(activatorRouterKey.name())
	if err != nil {
		t.Fatalf("协调者未按名注册: %v", err)
	}

	// 制造一次崩溃(非正常终止)。
	if err := node.SendExit(first, gen.TerminateReasonPanic); err != nil {
		t.Fatalf("kill router: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pid, qerr := node.ProcessPID(activatorRouterKey.name())
		if qerr == nil && pid != first {
			return // 已重建为新实例
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("协调者崩溃后未被重建: 旧 pid=%v 仍可查(%v)", first, err)
}
