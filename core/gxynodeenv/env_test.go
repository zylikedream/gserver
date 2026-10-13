package gxynodeenv

import (
	"testing"

	"gserver/core/gxyregistery/types"
)

func TestMapOKGOpsState(t *testing.T) {
	tests := []struct {
		name string
		ops  string
		want types.ServiceState
	}{
		{name: "empty is serving", ops: "", want: types.ServiceStateServing},
		{name: "none is serving", ops: "None", want: types.ServiceStateServing},
		{name: "allocated is serving", ops: "Allocated", want: types.ServiceStateServing},
		{name: "wait to be deleted is draining", ops: "WaitToBeDeleted", want: types.ServiceStateDraining},
		{name: "kill is draining", ops: "Kill", want: types.ServiceStateDraining},
		{name: "maintaining is maintaining", ops: "Maintaining", want: types.ServiceStateMaintaining},
		{name: "unknown is serving", ops: "Custom", want: types.ServiceStateServing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapOKGOpsState(tt.ops); got != tt.want {
				t.Fatalf("mapOKGOpsState(%q) = %q, want %q", tt.ops, got, tt.want)
			}
		})
	}
}
