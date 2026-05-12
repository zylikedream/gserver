package ets

import (
	"testing"
)

func TestETS_SetGet(t *testing.T) {
	e := NewETS("test")
	e.Set("key", "value")
	if got := e.Get("key"); got != "value" {
		t.Fatalf("expected value, got %v", got)
	}
}

func TestETS_GetMissing(t *testing.T) {
	e := NewETS("test")
	if got := e.Get("missing"); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestETS_GetDefault(t *testing.T) {
	e := NewETS("test")
	if got := e.Get("missing", "default"); got != "default" {
		t.Fatalf("expected default, got %v", got)
	}
}

func TestETS_GetExistingIgnoresDefault(t *testing.T) {
	e := NewETS("test")
	e.Set("key", 42)
	if got := e.Get("key", "default"); got != 42 {
		t.Fatalf("expected 42, got %v", got)
	}
}

func TestETS_Overwrite(t *testing.T) {
	e := NewETS("test")
	e.Set("key", "old")
	e.Set("key", "new")
	if got := e.Get("key"); got != "new" {
		t.Fatalf("expected new, got %v", got)
	}
}

func TestETS_DifferentTypes(t *testing.T) {
	e := NewETS("test")
	e.Set("int", 123)
	e.Set("str", "hello")
	e.Set("bool", true)
	if e.Get("int") != 123 || e.Get("str") != "hello" || e.Get("bool") != true {
		t.Fatalf("unexpected values: int=%v str=%v bool=%v", e.Get("int"), e.Get("str"), e.Get("bool"))
	}
}
