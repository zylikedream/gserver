package main

import (
	"testing"
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
