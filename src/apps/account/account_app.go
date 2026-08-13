package account

import (
	"context"

	"gserver/core/gxyapp"
	"gserver/core/gxypgx"
	"gserver/core/gxyservice"
	"gserver/src/apps/account/logic"
)

type accountApp struct {
	gxyapp.App
	host string
}

func NewAccountApp(host string) *accountApp {
	return &accountApp{host: host}
}

func (a *accountApp) OnModInit(ctx context.Context) error {
	if err := logic.InitAccountSchema(ctx, gxypgx.DB()); err != nil {
		return err
	}
	gxyservice.ServiceApp().LoadService(ctx, NewAccountService(a.host))
	return nil
}
