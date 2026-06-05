package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Addr            string           `yaml:"addr"`
	AccountPattern  string           `yaml:"account_pattern"`
	AccountServer   string           `yaml:"account_server,omitempty"`
	Platform        string           `yaml:"platform,omitempty"`
	TotalBots       int              `yaml:"total_bots"`
	StartupRate     int              `yaml:"startup_rate"`
	ReportInterval  time.Duration    `yaml:"report_interval"`
	LogFile         string           `yaml:"log_file"`
	Silent          bool             `yaml:"silent"`
	BotTypes        []BotTypeConfig  `yaml:"bot_types"`
	ChatMixin       *ChatMixinConfig `yaml:"chat_mixin,omitempty"`
}

type BotTypeConfig struct {
	ID     string       `yaml:"id"`
	Weight int          `yaml:"weight"`
	Script []ScriptStep `yaml:"script"`
}

type ScriptStep struct {
	Do   string
	Args map[string]interface{}
}

func (s *ScriptStep) UnmarshalYAML(value *yaml.Node) error {
	var m map[string]interface{}
	if err := value.Decode(&m); err != nil {
		return err
	}
	for k, v := range m {
		s.Do = k
		if v == nil {
			s.Args = map[string]interface{}{}
		} else {
			args, ok := v.(map[string]interface{})
			if !ok {
				return fmt.Errorf("script step %q: args must be a map", k)
			}
			s.Args = args
		}
		return nil
	}
	return fmt.Errorf("empty script step")
}

type ChatMixinConfig struct {
	Chance   float64  `yaml:"chance"`
	Channel  int32    `yaml:"channel"`
	Messages []string `yaml:"messages"`
}

type DurationRange struct {
	Min time.Duration
	Max time.Duration
}

func (d *DurationRange) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	// Parse "5s" or "3-8s"
	if dur, err := time.ParseDuration(s); err == nil {
		d.Min = dur
		d.Max = dur
		return nil
	}
	var minStr, maxStr string
	if n, _ := fmt.Sscanf(s, "%s-%s", &minStr, &maxStr); n == 2 {
		min, err := time.ParseDuration(minStr)
		if err != nil {
			return fmt.Errorf("invalid delay %q: %v", s, err)
		}
		max, err := time.ParseDuration(maxStr)
		if err != nil {
			return fmt.Errorf("invalid delay %q: %v", s, err)
		}
		d.Min = min
		d.Max = max
		return nil
	}
	return fmt.Errorf("invalid delay %q: expected duration like \"5s\" or range like \"3-8s\"", s)
}

func (d DurationRange) Random() time.Duration {
	if d.Min >= d.Max {
		return d.Min
	}
	return d.Min + time.Duration(rand.Int63n(int64(d.Max-d.Min+1)))
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.TotalBots <= 5 {
		cfg.Silent = false
	}
	if cfg.Addr == "" {
		return nil, fmt.Errorf("addr is required")
	}
	if cfg.AccountPattern == "" {
		return nil, fmt.Errorf("account_pattern is required")
	}
	if cfg.TotalBots <= 0 {
		return nil, fmt.Errorf("total_bots must be > 0")
	}
	if len(cfg.BotTypes) == 0 {
		return nil, fmt.Errorf("bot_types is required")
	}
	totalWeight := 0
	for _, bt := range cfg.BotTypes {
		if len(bt.Script) == 0 {
			return nil, fmt.Errorf("bot_type %q: script is empty", bt.ID)
		}
		totalWeight += bt.Weight
	}
	if totalWeight <= 0 {
		return nil, fmt.Errorf("total weight must be > 0")
	}
	if cfg.ReportInterval <= 0 {
		cfg.ReportInterval = 5 * time.Second
	}
	return &cfg, nil
}

func (cfg *Config) BotTypeFor(index int) *BotTypeConfig {
	totalWeight := 0
	for _, bt := range cfg.BotTypes {
		totalWeight += bt.Weight
	}
	pos := index % totalWeight
	for _, bt := range cfg.BotTypes {
		pos -= bt.Weight
		if pos < 0 {
			return &BotTypeConfig{
				ID:     bt.ID,
				Weight: bt.Weight,
				Script: bt.Script,
			}
		}
	}
	return &BotTypeConfig{
		ID:     cfg.BotTypes[0].ID,
		Weight: cfg.BotTypes[0].Weight,
		Script: cfg.BotTypes[0].Script,
	}
}
