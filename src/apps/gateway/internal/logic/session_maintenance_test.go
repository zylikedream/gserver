package logic

import (
	"context"
	"strings"
	"testing"

	"gserver/core/gxyactor"
	"gserver/protocol/pb"

	"github.com/asynkron/protoactor-go/actor"
)

func TestHandleHandshakeRejectsMaintenanceBeforeActivatingRole(t *testing.T) {
	oldMaintenance := gateMaintenanceEnabled
	oldGetRoleID := getRoleIDByAccount
	oldActivateRole := activateRole
	t.Cleanup(func() {
		gateMaintenanceEnabled = oldMaintenance
		getRoleIDByAccount = oldGetRoleID
		activateRole = oldActivateRole
	})

	activated := false
	gateMaintenanceEnabled = func() bool { return true }
	getRoleIDByAccount = func(ctx context.Context, account string) (int64, error) {
		return 1001, nil
	}
	activateRole = func(ctx context.Context, roleID int64) (gxyactor.PID, error) {
		activated = true
		return actor.NewPID("local", "1001"), nil
	}

	s := NewSession(nil)
	err := s.handleHandshake(context.Background(), &pb.ReqHandShake{AccountUid: "acct-1"})
	if err == nil {
		t.Fatal("handleHandshake() error = nil, want maintenance error")
	}
	if !strings.Contains(err.Error(), "maintenance") {
		t.Fatalf("handleHandshake() error = %q, want maintenance", err.Error())
	}
	if activated {
		t.Fatal("handleHandshake() activated role while gate is in maintenance")
	}
}
