package logic

// RoleGM execCommand 边界测试: 参数不足返回友好错误而非 reflect panic。

import (
	"strings"
	"testing"
)

// TestExecCommand_TooFewArgs 参数不足: 返回错误, 不 panic(此前 reflect.Call 直接 panic)。
func TestExecCommand_TooFewArgs(t *testing.T) {
	r := &RoleGM{}
	err := r.execCommand("set_player_level", nil) // 方法需要 1 个参数
	if err == nil {
		t.Fatal("expected arg-count error")
	}
	if !strings.Contains(err.Error(), "needs 1 arg") {
		t.Fatalf("expected friendly arg-count message, got %v", err)
	}
}

// TestExecCommand_UnknownCommand 未知命令返回错误。
func TestExecCommand_UnknownCommand(t *testing.T) {
	r := &RoleGM{}
	if err := r.execCommand("no_such_cmd", nil); err == nil {
		t.Fatal("expected unknown command error")
	}
}
