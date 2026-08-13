package chat

// chat lobby 测试:miniredis 执行真实 lua 脚本,验证入厅/离厅/容量语义。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/src/pkg/deps"
	"gserver/src/pkg/gameconfig"
)

func loadGlobalConfig(t *testing.T) *gamecfg.Tables {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "gameconfig/json/garden_tbglobalconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	tbGlobal, err := gamecfg.NewGardenTbGlobalConfig(rows)
	if err != nil {
		t.Fatal(err)
	}
	return &gamecfg.Tables{TbGlobalConfig: tbGlobal}
}

func newChatTestDeps(t *testing.T) (deps.Deps, *miniredis.Miniredis) {
	t.Helper()
	srv := miniredis.RunT(t)
	cli := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = cli.Close() })
	return deps.Deps{
		Redis: cli,
		Cfg:   &gameconfig.GameConfig{Tables: loadGlobalConfig(t)},
	}, srv
}

func lobbySize(t *testing.T, srv *miniredis.Miniredis, lobbyID string) int {
	t.Helper()
	s, err := srv.SCard("chat:lobby:" + lobbyID)
	if err != nil {
		t.Fatalf("scard: %v", err)
	}
	return s
}

func TestJoinLobby_FirstPlayerCreatesLobby(t *testing.T) {
	d, srv := newChatTestDeps(t)
	ctx := context.Background()

	lobbyID, err := JoinLobby(ctx, d, 1001)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if lobbyID != 1 {
		t.Fatalf("expected lobby 1, got %d", lobbyID)
	}
	if size := lobbySize(t, srv, "1"); size != 1 {
		t.Fatalf("expected size 1, got %d", size)
	}
}

func TestJoinLobby_SameLobbyUntilFull(t *testing.T) {
	d, srv := newChatTestDeps(t)
	ctx := context.Background()

	// 前 500 人都在 lobby 1(maxPlayers=500)
	var firstID int64 = -1
	for i := 1; i <= 500; i++ {
		id, err := JoinLobby(ctx, d, int64(i))
		if err != nil {
			t.Fatalf("join %d: %v", i, err)
		}
		if firstID == -1 {
			firstID = id
		}
		if id != firstID {
			t.Fatalf("player %d expected lobby %d, got %d", i, firstID, id)
		}
	}
	if size := lobbySize(t, srv, "1"); size != 500 {
		t.Fatalf("expected lobby 1 size 500, got %d", size)
	}

	// 第 501 人进新 lobby
	id, err := JoinLobby(ctx, d, 501)
	if err != nil {
		t.Fatalf("join 501: %v", err)
	}
	if id == firstID {
		t.Fatal("expected new lobby for overflow player")
	}
	if size := lobbySize(t, srv, "2"); size != 1 {
		t.Fatalf("expected lobby 2 size 1, got %d", size)
	}
}

func TestJoinLobby_ReusesFreedSlot(t *testing.T) {
	d, srv := newChatTestDeps(t)
	ctx := context.Background()

	lobbyID, err := JoinLobby(ctx, d, 1001)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := LeaveLobby(ctx, d, 1001, lobbyID); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if size := lobbySize(t, srv, "1"); size != 0 {
		t.Fatalf("expected size 0 after leave, got %d", size)
	}

	// 离开后新玩家复用 lobby 1 的空位
	id, err := JoinLobby(ctx, d, 2001)
	if err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	if id != lobbyID {
		t.Fatalf("expected reuse lobby %d, got %d", lobbyID, id)
	}
}

func TestLeaveLobby_UnknownLobby(t *testing.T) {
	d, _ := newChatTestDeps(t)
	ctx := context.Background()

	if err := LeaveLobby(ctx, d, 1001, 999); err != nil {
		t.Fatalf("leave unknown lobby should not error: %v", err)
	}
}
