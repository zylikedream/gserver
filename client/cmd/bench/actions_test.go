package main

import (
	"testing"
	"time"
)

func TestParseIntArg(t *testing.T) {
	args := map[string]interface{}{"flower_id": 101}
	v := getIntArg(args, "flower_id")
	if v != 101 {
		t.Errorf("got %d, want 101", v)
	}
}

func TestParseIntSliceArg(t *testing.T) {
	args := map[string]interface{}{"plot_ids": []interface{}{1, 2, 3}}
	v := getIntSliceArg(args, "plot_ids")
	if len(v) != 3 || v[0] != 1 || v[1] != 2 || v[2] != 3 {
		t.Errorf("got %v, want [1 2 3]", v)
	}
}

func TestParseFloatArg(t *testing.T) {
	args := map[string]interface{}{"min": 0.0, "max": 5.0}
	min := getFloatArg(args, "min")
	max := getFloatArg(args, "max")
	if min != 0 || max != 5 {
		t.Errorf("got min=%f max=%f", min, max)
	}
}
func TestPlotRequestPacerEnforcesFiftyMillisecondGap(t *testing.T) {
	current := time.Unix(0, 0)
	var slept []time.Duration
	pacer := plotRequestPacer{
		now: func() time.Time { return current },
		sleep: func(d time.Duration) {
			slept = append(slept, d)
			current = current.Add(d)
		},
	}

	pacer.wait()
	pacer.wait()

	if len(slept) != 1 || slept[0] != 50*time.Millisecond {
		t.Fatalf("sleep calls = %v, want [50ms]", slept)
	}
}
