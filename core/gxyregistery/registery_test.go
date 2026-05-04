package gxyregistery

import (
	"context"
	"gserver/core/gxyutil"
	"os"
	"testing"

	"github.com/gogf/gf/v2/encoding/gjson"
)

func TestRegistery_Search(t *testing.T) {
	if os.Getenv("RUN_REGISTERY_TESTS") != "1" {
		t.Skip("set RUN_REGISTERY_TESTS=1 to run registry integration tests")
	}
	gxyutil.SetConfig("config/service.test.toml")
	registry, err := NewRegistery()
	if err != nil {
		t.Fatalf("new registery failed:%+v", err)
	}
	services, err := registry.Search(context.Background(), "role")
	if err != nil {
		t.Errorf("search role failed:%+v", err)
	}
	t.Logf("search role success, services: %+v", gjson.MustEncodeString(services))
}
