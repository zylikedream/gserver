package main

import (
	"os"
	"testing"
)

func TestLoadConfigMinimal(t *testing.T) {
	data := []byte(`
addr: "127.0.0.1:11086"
account_pattern: "test_%d"
total_bots: 100
bot_types:
  - id: newbie
    weight: 100
    script:
      - login: {}
      - wait_range: {min: 0, max: 5}
`)
	path := writeTempConfig(t, data)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:11086" {
		t.Errorf("addr = %q", cfg.Addr)
	}
	if cfg.TotalBots != 100 {
		t.Errorf("total = %d", cfg.TotalBots)
	}
	if len(cfg.BotTypes) != 1 {
		t.Fatalf("bot_types = %d", len(cfg.BotTypes))
	}
	if cfg.BotTypes[0].ID != "newbie" {
		t.Errorf("id = %q", cfg.BotTypes[0].ID)
	}
	if len(cfg.BotTypes[0].Script) != 2 {
		t.Fatalf("script steps = %d", len(cfg.BotTypes[0].Script))
	}
	if cfg.BotTypes[0].Script[0].Do != "login" {
		t.Errorf("step[0].Do = %q", cfg.BotTypes[0].Script[0].Do)
	}
}

func TestLoadConfigWithChatMixin(t *testing.T) {
	data := []byte(`
addr: "127.0.0.1:11086"
account_pattern: "test_%d"
total_bots: 100
bot_types:
  - id: newbie
    weight: 100
    script:
      - login: {}
chat_mixin:
  chance: 0.1
  channel: 1
  messages: ["hello", "world"]
`)
	path := writeTempConfig(t, data)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatMixin == nil {
		t.Fatal("chat_mixin is nil")
	}
	if cfg.ChatMixin.Chance != 0.1 {
		t.Errorf("chance = %f", cfg.ChatMixin.Chance)
	}
	if cfg.ChatMixin.Channel != 1 {
		t.Errorf("channel = %d", cfg.ChatMixin.Channel)
	}
	if len(cfg.ChatMixin.Messages) != 2 {
		t.Errorf("messages = %d", len(cfg.ChatMixin.Messages))
	}
}

func writeTempConfig(t *testing.T, data []byte) string {
	t.Helper()
	f, err := os.CreateTemp("", "bench-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}
