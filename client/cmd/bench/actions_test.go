package main

import (
	"testing"
	"time"
)

// TestParseArgs:一张表覆盖 getIntArg / getIntSliceArg / getFloatArg 三种取值方式。
// 为什么需要:压测参数全是从 map[string]any 里取,类型判断一旦写错,
// 缺失或错类型的参数会静默变成 0,机器人会对着空背包空跑一整场。
func TestParseArgs(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		v := getIntArg(map[string]any{"flower_id": 101}, "flower_id")
		if v != 101 {
			t.Errorf("got %d, want 101", v)
		}
	})
	t.Run("int slice", func(t *testing.T) {
		v := getIntSliceArg(map[string]any{"plot_ids": []any{1, 2, 3}}, "plot_ids")
		if len(v) != 3 || v[0] != 1 || v[1] != 2 || v[2] != 3 {
			t.Errorf("got %v, want [1 2 3]", v)
		}
	})
	t.Run("float", func(t *testing.T) {
		args := map[string]any{"min": 0.0, "max": 5.0}
		min := getFloatArg(args, "min")
		max := getFloatArg(args, "max")
		if min != 0 || max != 5 {
			t.Errorf("got min=%f max=%f", min, max)
		}
	})
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
