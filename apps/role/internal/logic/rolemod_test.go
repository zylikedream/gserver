package logic

import (
	"context"
	"testing"
)

func TestRoleModule(t *testing.T) {
	ctx := context.Background()
	r := NewRoleMain()
	r.initRoleModules(ctx)
	t.Logf("role basic : %v", r.Basic)
}
