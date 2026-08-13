package chat

// chat lobby 测试:miniredis 执行真实 lua 脚本,验证入厅/离厅/容量语义。

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/src/pkg/deps"
	"gserver/src/pkg/gameconfig"
)

// loadGlobalConfig 构造自含配表(单行合成,不依赖仓库 json,配表变更不脆断)。
func loadGlobalConfig(t *testing.T) *gamecfg.Tables {
	t.Helper()
	rows := []map[string]any{{
		"world_chat_lobby_max_players": float64(500),
		"init_items":                   []any{},
		"max_energy":                   float64(100),
		"energy_recover_sec":           float64(60),
		"bag_max_cells":                float64(100),
	}}
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
	maxPlayers := int(d.Cfg.TbGlobalConfig.Get().WorldChatLobbyMaxPlayers)

	// 前 maxPlayers 人都在 lobby 1
	var firstID int64 = -1
	for i := 1; i <= maxPlayers; i++ {
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
	if size := lobbySize(t, srv, "1"); size != maxPlayers {
		t.Fatalf("expected lobby 1 size %d, got %d", maxPlayers, size)
	}

	// 溢出玩家进新 lobby
	id, err := JoinLobby(ctx, d, int64(maxPlayers+1))
	if err != nil {
		t.Fatalf("join overflow: %v", err)
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
