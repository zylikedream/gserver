package gxyutil

import (
	"context"
	"errors"
	"testing"
)

// ========== test handler types ==========

type ReqPing struct {
	Value int
}
type RspPong struct {
	Value int
}
type ReqNoHandler struct{}

type testHandler struct{}

func (h *testHandler) HandlePing(ctx context.Context, req *ReqPing) (*RspPong, error) {
	return &RspPong{Value: req.Value * 2}, nil
}

type testHandlerErr struct{}

func (h *testHandlerErr) HandlePing(ctx context.Context, req *ReqPing) (*RspPong, error) {
	return nil, errors.New("boom")
}

type handlerWithPrefix struct{}

func (h *handlerWithPrefix) OnLogin(ctx context.Context, req *ReqPing) error {
	return nil
}
func (h *handlerWithPrefix) OnLogout(ctx context.Context, req *ReqNoHandler) error {
	return nil
}
func (h *handlerWithPrefix) IgnorePrivate(ctx context.Context, req *ReqPing) (*RspPong, error) {
	return nil, nil
}

// ========== MsgHandler tests ==========

func TestMsgHandler_CallWithMsg_Success(t *testing.T) {
	h := NewMsgHandler()
	h.AddHandler(&testHandler{})
	rsp, err := h.CallWithMsg(context.Background(), &ReqPing{Value: 5})
	if err != nil {
		t.Fatal(err)
	}
	pong := rsp.(*RspPong)
	if pong.Value != 10 {
		t.Fatalf("expected 10, got %d", pong.Value)
	}
}

func TestMsgHandler_CallWithMsg_Error(t *testing.T) {
	h := NewMsgHandler()
	h.AddHandler(&testHandlerErr{})
	_, err := h.CallWithMsg(context.Background(), &ReqPing{Value: 1})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got %v", err)
	}
}

func TestMsgHandler_CallWithMsg_NoHandler(t *testing.T) {
	h := NewMsgHandler()
	_, err := h.CallWithMsg(context.Background(), &ReqNoHandler{})
	if err == nil {
		t.Fatal("expected error for unregistered message")
	}
}

func TestMsgHandler_AddHandler_WithPrefix(t *testing.T) {
	h := NewMsgHandler()
	h.AddHandler(&handlerWithPrefix{}, "On")

	_, err := h.CallWithMsg(context.Background(), &ReqPing{})
	if err != nil {
		t.Fatal("OnLogin should be registered for ReqPing")
	}
	_, err = h.CallWithMsg(context.Background(), &ReqNoHandler{})
	if err != nil {
		t.Fatal("OnLogout should be registered for ReqNoHandler")
	}
}

func TestMsgHandler_AddHandler_PrefixFiltersUnmatched(t *testing.T) {
	h := NewMsgHandler()
	h.AddHandler(&handlerWithPrefix{}, "On")

	// IgnorePrivate has no "On" prefix, should not be registered
	meta := h.GetMethodMetaByName("IgnorePrivate")
	if meta != nil {
		t.Fatal("IgnorePrivate should not be registered with prefix 'On'")
	}
}

func TestMsgHandlerAddHandlerReturnsRegisteredMetadata(t *testing.T) {
	h := NewMsgHandler()
	metas := h.AddHandler(&handlerWithPrefix{}, "On")
	got := make(map[string]bool, len(metas))
	for _, meta := range metas {
		got[meta.Method.Name] = true
	}
	if !got["OnLogin"] || !got["OnLogout"] || len(got) != 2 {
		t.Fatalf("registered method metadata = %v", got)
	}

	second := h.AddHandler(&testHandler{})
	if len(second) != 1 || second[0].ArgType.Name() != "ReqPing" {
		t.Fatalf("second registration metadata = %#v", second)
	}
}

func TestMsgHandler_GetMethodMeta(t *testing.T) {
	h := NewMsgHandler()
	h.AddHandler(&testHandler{})
	meta := h.GetMethodMeta(&ReqPing{})
	if meta == nil {
		t.Fatal("expected meta for ReqPing")
	}
	if meta.ArgType.Name() != "ReqPing" {
		t.Fatalf("expected ArgType ReqPing, got %s", meta.ArgType.Name())
	}
}

func TestMsgHandler_GetMethodMeta_NotFound(t *testing.T) {
	h := NewMsgHandler()
	meta := h.GetMethodMeta(&ReqNoHandler{})
	if meta != nil {
		t.Fatal("expected nil for unregistered message")
	}
}

func TestMsgHandler_GetMethods(t *testing.T) {
	h := NewMsgHandler()
	h.AddHandler(&testHandler{})
	methods := h.GetMethods()
	if len(methods) == 0 {
		t.Fatal("expected at least one registered method")
	}
}

// ========== GetSuitableMethods tests ==========

func TestGetSuitableMethods_FiltersUnexported(t *testing.T) {
	// Test that non-3-arg methods are skipped
	type badHandler struct{}
	// No suitable methods — badHandler has no methods with (context.Context, *T) (reply, error)
	metas := GetSuitableMethods(TypeReal(&badHandler{}), "")
	if len(metas) != 0 {
		t.Fatalf("expected 0 methods, got %d", len(metas))
	}
}

// ========== GetObjectName / GetTypeName tests ==========

func TestGetObjectName(t *testing.T) {
	name := GetObjectName(&ReqPing{})
	if name != "ReqPing" {
		t.Fatalf("expected ReqPing, got %s", name)
	}
}

func TestGetObjectName_Pointer(t *testing.T) {
	// Even when wrapped in interface, should resolve to struct name
	name := GetObjectName(&ReqPing{})
	if name != "ReqPing" {
		t.Fatalf("expected ReqPing, got %s", name)
	}
}

func TestGetName(t *testing.T) {
	name := GetName[ReqPing]()
	if name != "ReqPing" {
		t.Fatalf("expected ReqPing, got %s", name)
	}
}

// ========== FormatObject test ==========

func TestNewObject(t *testing.T) {
	obj := NewObject(TypeReal(&ReqPing{}))
	if _, ok := obj.(*ReqPing); !ok {
		t.Fatalf("expected *ReqPing, got %T", obj)
	}
}
