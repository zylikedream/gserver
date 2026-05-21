package gxynodeenv

import (
	"testing"

	"gserver/core/gxyregistery"
)

func TestMapOKGOpsState(t *testing.T) {
	tests := []struct {
		name string
		ops  string
		want gxyregistery.ServiceState
	}{
		{name: "empty is serving", ops: "", want: gxyregistery.ServiceStateServing},
		{name: "none is serving", ops: "None", want: gxyregistery.ServiceStateServing},
		{name: "allocated is serving", ops: "Allocated", want: gxyregistery.ServiceStateServing},
		{name: "wait to be deleted is draining", ops: "WaitToBeDeleted", want: gxyregistery.ServiceStateDraining},
		{name: "kill is draining", ops: "Kill", want: gxyregistery.ServiceStateDraining},
		{name: "maintaining is maintaining", ops: "Maintaining", want: gxyregistery.ServiceStateMaintaining},
		{name: "unknown is serving", ops: "Custom", want: gxyregistery.ServiceStateServing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapOKGOpsState(tt.ops); got != tt.want {
				t.Fatalf("mapOKGOpsState(%q) = %q, want %q", tt.ops, got, tt.want)
			}
		})
	}
}
