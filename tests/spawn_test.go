package tests

import (
	"gserver/core/gxyactor"
	"gserver/protocol/pb"
	"testing"

	"ergo.services/ergo"
	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"ergo.services/ergo/net/edf"
)

type TestSession struct {
	gxyactor.ActorBase
}

type TestSession1 struct {
	act.Actor
}

func (a *TestSession1) HandleMessage(from gen.PID, message any) error {
	return nil
}

func CreateTestSession() gen.ProcessBehavior {
	return &TestSession{}
}

func CreateTestSession1() gen.ProcessBehavior {
	return &TestSession1{}
}

func TestSpawn_SpawnSession(t *testing.T) {
	node, err := ergo.StartNode("test@localhost", gen.NodeOptions{
		Network: gen.NetworkOptions{
			Cookie: "test",
		},
	})

	session := CreateTestSession()
	_, ok := session.(act.ActorBehavior)
	t.Log("is processBehavior", ok)
	if err != nil {
		t.Errorf("StartNode() error = %v", err)
		return
	}
	pid, err := node.Spawn(func() gen.ProcessBehavior {
		return CreateTestSession()
	}, gen.ProcessOptions{})
	if err != nil {
		t.Errorf("SpawnSession() error = %v", err)
		return
	}
	t.Logf("SpawnSession() pid = %v", pid)
}

func TestRegister_proto(t *testing.T) {
	err := edf.RegisterTypeOf(&pb.Ack{})
	t.Logf("------------- register pointer ack %s", err)
	err = edf.RegisterTypeOf(pb.Ack{})
	t.Logf("------------- register ack %s", err)
}
