package ets

import (
	"testing"
)

func TestETS_OverwriteKeepsLatestPerKey(t *testing.T) {
	e := NewETS("test")
	e.Set("int", 123)
	e.Set("str", "hello")
	e.Set("bool", true)
	e.Set("str", "world")

	if e.Get("int") != 123 || e.Get("str") != "world" || e.Get("bool") != true {
		t.Fatalf("unexpected values: int=%v str=%v bool=%v", e.Get("int"), e.Get("str"), e.Get("bool"))
	}
}
