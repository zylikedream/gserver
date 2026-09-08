package gxyactor_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"ergo.services/ergo/gen"
	"gserver/core/gxyactor"
	ergo "gserver/core/gxyactor/internal/ergo"
	"gserver/protocol/pb"
	"gserver/src/apps/chat"
)

func TestErgoChannelTwoNodeActivationSendCallTerminateReactivate(t *testing.T) {
	port := freeTCPPort(t)
	const cookie = "gserver-channel-stage"
	a, err := ergo.Start(ergo.Options{
		NodeName:         "channel-stage-a@localhost",
		NodeInstanceName: "channel-stage-a@localhost",
		Host:             "127.0.0.1",
		Port:             port,
		Cookie:           cookie,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.StopNode(time.Second)

	if err := a.RegisterActorKind("chat_channel", func() gxyactor.IActor {
		return chat.NewChannelActor()
	}); err != nil {
		t.Fatal(err)
	}

	var directory struct {
		sync.Mutex
		pid gxyactor.PID
		raw gen.PID
	}
	activate := func(_ context.Context, kind, id string, allowSpawn bool) (gxyactor.PID, error) {
		if !allowSpawn {
			return gxyactor.PID{}, errors.New("channel activation requires spawn permission")
		}
		directory.Lock()
		defer directory.Unlock()
		if local := a.GetLocalActor(kind, id); !local.IsZero() {
			directory.pid = local
			raw, resolveErr := a.PIDToErgo(local)
			if resolveErr != nil {
				return gxyactor.PID{}, resolveErr
			}
			directory.raw = raw
			return local, nil
		}
		pid, spawnErr := a.Spawn(kind, id, id)
		if spawnErr != nil {
			return gxyactor.PID{}, spawnErr
		}
		raw, resolveErr := a.PIDToErgo(pid)
		if resolveErr != nil {
			return gxyactor.PID{}, resolveErr
		}
		directory.pid, directory.raw = pid, raw
		return pid, nil
	}

	bPort := freeTCPPort(t)
	b, err := ergo.Start(ergo.Options{
		NodeName:         "channel-stage-b@localhost",
		NodeInstanceName: "channel-stage-b@localhost",
		Host:             "127.0.0.1",
		Port:             bPort,
		Cookie:           cookie,
		Activation:       activate,
		ResolvePID: func(pid gxyactor.PID) (gen.PID, error) {
			directory.Lock()
			defer directory.Unlock()
			if pid != directory.pid {
				return gen.PID{}, errors.New("directory PID mismatch")
			}
			return directory.raw, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gxyactor.SetRuntime(nil) })
	defer b.StopNode(time.Second)
	if err := b.Node().Network().AddRoute("channel-stage-a@localhost", gen.NetworkRoute{
		Route: gen.Route{Host: "127.0.0.1", Port: uint16(port)},
	}, 1); err != nil {
		t.Fatal(err)
	}

	pid, err := b.ActivateActor(context.Background(), "chat_channel", "1_100", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Send(context.Background(), pid, &pb.ChannelRegisterMsg{
		RoleId: 0,
		Pid:    &pb.ActorPid{Address: "channel-stage-b@localhost", Id: "role/0"},
		ChannelType: 1,
		ChannelId: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Send(context.Background(), pid, &pb.ChannelUnregisterMsg{
		RoleId: 0, ChannelType: 1, ChannelId: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Send(context.Background(), pid, &pb.ReqChannelSend{
		ChannelType: 1, ChannelId: 100, SenderId: 7, Content: "remote-send",
	}); err != nil {
		t.Fatal(err)
	}

	waitForChannelHistory(t, b, pid, "remote-send")

	if err := a.Stop(pid); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !a.GetLocalActor("chat_channel", "1_100").IsZero() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !a.GetLocalActor("chat_channel", "1_100").IsZero() {
		t.Fatal("channel remained published after termination")
	}

	reactivated, err := b.ActivateActor(context.Background(), "chat_channel", "1_100", true)
	if err != nil {
		t.Fatal(err)
	}
	if reactivated.IsZero() {
		t.Fatalf("reactivated PID is zero")
	}
	if err := b.Send(context.Background(), reactivated, &pb.ReqChannelSend{
		ChannelType: 1, ChannelId: 100, SenderId: 8, Content: "after-reactivation",
	}); err != nil {
		t.Fatal(err)
	}
	waitForChannelHistory(t, b, reactivated, "after-reactivation")
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
func waitForChannelHistory(t *testing.T, adapter *ergo.Adapter, pid gxyactor.PID, content string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		result, err := adapter.Call(context.Background(), pid, &pb.ReqChatChannelHistory{
			ChannelType: 1, ChannelId: 100, Count: 10,
		}, time.Second)
		if err == nil {
			if history, ok := result.(*pb.RspChatChannelHistory); ok && len(history.Messages) == 1 &&
				history.Messages[0].Content == content {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for channel history %q", content)
}
