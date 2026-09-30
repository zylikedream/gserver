package logic

import (
	"context"

	"gserver/src/lib/gatetoken"
)

type AccountHandler struct {
	Service *Service
	Config  PreloginConfig
	Signer  gatetoken.Signer
}

func (h *AccountHandler) Prelogin(ctx context.Context, req *PreloginRequest) (any, error) {
	return h.Service.BuildPreloginResponse(ctx, h.Config, h.Signer, req.Platform, req.PlatformUID, req.ClientVersion)
}
