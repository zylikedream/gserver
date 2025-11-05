package gxyregistery

import (
	"context"
	"gserver/util"
	"testing"

	"github.com/gogf/gf/v2/encoding/gjson"
)

func TestRegistery_Search(t *testing.T) {
	util.SetConfig("config/service.test.toml")
	registry, err := NewRegistery()
	if err != nil {
		t.Errorf("new registery failed:%+v", err)
	}
	services, err := registry.Search(context.Background(), "role")
	if err != nil {
		t.Errorf("search role failed:%+v", err)
	}
	t.Logf("search role success, services: %+v", gjson.MustEncodeString(services))
}
