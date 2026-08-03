package thanks

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxylog"
)

type thanksApp struct {
	gxyapp.App
}

func NewThanksApp() *thanksApp {
	return &thanksApp{}
}

func (s *thanksApp) OnModStartAfter(ctx context.Context) error {
	gxylog.Info(ctx, "server started, thanks for using GServer")
	return nil
}
