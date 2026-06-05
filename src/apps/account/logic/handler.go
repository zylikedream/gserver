package logic

import (
	"context"

	"gserver/src/lib/gatetoken"
)

type AccountHandler struct {
	Config PreloginConfig
	Signer gatetoken.Signer
}

func (h *AccountHandler) Prelogin(ctx context.Context, req *PreloginRequest) (any, error) {
	return BuildPreloginResponse(ctx, h.Config, h.Signer, req.Platform, req.PlatformUID, req.ClientVersion)
}
