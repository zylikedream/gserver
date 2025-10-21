package gxyregistery

import (
	"context"
	"testing"

	"github.com/gogf/gf/v2/encoding/gjson"
)

func TestRegistery_Search(t *testing.T) {
	registry, err := NewRegistery(REGISTERY_TYPE_CONSUL, "config/service.test.toml")
	if err != nil {
		t.Errorf("new registery failed:%+v", err)
	}
	services, err := registry.Search(context.Background(), "role")
	if err != nil {
		t.Errorf("search role failed:%+v", err)
	}
	t.Logf("search role success, services: %+v", gjson.MustEncodeString(services))
}
