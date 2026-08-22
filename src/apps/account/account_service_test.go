package account

import (
	"context"
	"testing"
)

func TestAccountServiceMetadataAndStopBeforeStart(t *testing.T) {
	service := NewAccountService("127.0.0.1")
	if service.host != "127.0.0.1" {
		t.Fatalf("unexpected host: %q", service.host)
	}
	if service.ServiceName() != "account" {
		t.Fatalf("unexpected service name: %q", service.ServiceName())
	}
	if err := service.OnModStop(context.Background()); err != nil {
		t.Fatalf("stop before start: %v", err)
	}
}
