package gatetoken

import (
	"context"

	"gserver/core/gxyutil"

	"github.com/gogf/gf/v2/frame/g"
)

func LoadConfigFromGF(ctx context.Context) (*Config, error) {
	cfg := &Config{}
	if err := gxyutil.CfgUnmarshalKey(ctx, g.Cfg(), "token", cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
