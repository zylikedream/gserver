package gxyactor

import (
	"context"
	"testing"
	"time"

	"gserver/protocol/pb"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
)

// callProbe 在初始化期间向目标发起一次同步调用,把结果留给测试断言。
// 用真实的 actor 实例而非手工拼装的半成品:Call 是绑定进程后的对外操作。
type callProbe struct {
	*Actor

	target  PID
	result  any
	callErr error
}

func newCallProbe(target PID) *callProbe {
	p := &callProbe{target: target}
	p.Actor = NewActor()
	return p
}

// Init 是同步初始化段:基类环境已由门面备好,此处只做自己的事。
func (p *callProbe) Init(args ...any) error {
	p.result, p.callErr = p.Call(p.target, &pb.Ack{}, 5*time.Second)
	return nil
}

// stubOwnership 注入内存所有权载体,使测试不依赖 Redis。
func stubOwnership(t *testing.T) {
	t.Helper()
	t.Cleanup(SetOwnershipStore(memOwnershipStore{owner: ActorOwner{NodeID: "test@localhost", Epoch: 1}}))
}

// memOwnershipStore 是所有权载体的内存实现。
type memOwnershipStore struct{ owner ActorOwner }

func (m memOwnershipStore) Claim(context.Context, string, string) (ActorOwner, error) {
	return m.owner, nil
}

func (m memOwnershipStore) Locate(context.Context, string, string) (ActorOwner, error) {
	return m.owner, nil
}

func (m memOwnershipStore) Release(context.Context, string, string, ActorOwner) (bool, error) {
	return true, nil
}

// runProbe 创建探针 actor,先装对端应答桩再执行初始化(Prepare/Run 两段式),
// 返回初始化完成后的探针。
func runProbe(t *testing.T, stub func(*unit.Subject, gen.PID)) *callProbe {
	t.Helper()
	raw := gen.PID{Node: "unit@localhost", ID: 99}
	var probe *callProbe
	subj := unit.Prepare(t, ActorFactory("call_probe", func() Business {
		probe = newCallProbe(pidFromLocal(raw))
		return probe
	}), gen.ProcessOptions{})
	stub(subj, raw)
	if err := subj.Run(); err != nil {
		t.Fatalf("run probe: %v", err)
	}
	return probe
}

// TestActorCallReturnsBusinessError 对端把业务失败作为错误载荷返回时,调用方
// 必须拿到 error(见 invariants #9)。不还原的话业务拒绝会以"调用成功"的形态
// 抵达调用方,调用方只能靠类型断言去猜,猜错即 panic。
func TestActorCallReturnsBusinessError(t *testing.T) {
	stubOwnership(t)
	const reason = "你已申请过该公会"
	probe := runProbe(t, func(s *unit.Subject, raw gen.PID) {
		s.OnCall(raw).Respond(&pb.ActorError{Reason: reason})
	})

	if probe.callErr == nil {
		t.Fatalf("业务失败必须以 error 返回,实际 result=%#v err=nil", probe.result)
	}
	if got := probe.callErr.Error(); got != reason {
		t.Fatalf("error = %q, want %q", got, reason)
	}
	if probe.result != nil {
		t.Fatalf("业务失败时不应返回载荷,实际 %#v", probe.result)
	}
}

// TestActorCallReturnsResponse 正常响应必须原样返回,不得被当成错误。
func TestActorCallReturnsResponse(t *testing.T) {
	stubOwnership(t)
	want := &pb.Ack{Code: pb.AckCode_ACK_CODE_OK, Reason: "ok"}
	probe := runProbe(t, func(s *unit.Subject, raw gen.PID) {
		s.OnCall(raw).Respond(want)
	})

	if probe.callErr != nil {
		t.Fatalf("正常响应不应报错: %v", probe.callErr)
	}
	got, ok := probe.result.(*pb.Ack)
	if !ok {
		t.Fatalf("响应类型 = %T, want *pb.Ack", probe.result)
	}
	if got.Reason != want.Reason {
		t.Fatalf("响应内容 = %q, want %q", got.Reason, want.Reason)
	}
}
