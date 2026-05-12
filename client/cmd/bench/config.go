package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Addr            string        `yaml:"addr"`
	Bots            int           `yaml:"bots"`
	AccountPattern  string        `yaml:"account_pattern"`
	Scenario        []Action      `yaml:"scenario"`
	ReportInterval  time.Duration `yaml:"report_interval"`
}

type Action struct {
	Msg    string                 `yaml:"msg"`
	Fields map[string]interface{} `yaml:"fields,omitempty"`
	Delay  DurationRange          `yaml:"delay"`
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
		return nil, fmt.Errorf("read config: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %v", err)
	}
	if cfg.Addr == "" {
		return nil, fmt.Errorf("addr is required")
	}
	if cfg.Bots <= 0 {
		return nil, fmt.Errorf("bots must be > 0")
	}
	if cfg.AccountPattern == "" {
		return nil, fmt.Errorf("account_pattern is required")
	}
	if len(cfg.Scenario) == 0 {
		return nil, fmt.Errorf("scenario is required")
	}
	if cfg.ReportInterval <= 0 {
		cfg.ReportInterval = 5 * time.Second
	}
	return &cfg, nil
}
