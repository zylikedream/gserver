package friend

import (
	"context"
	"gserver/core/gxyhttp"
	"gserver/core/gxylog"
	"gserver/core/gxyservice"
	fl "gserver/src/apps/friend/logic"
)

type friendService struct {
	gxyhttp.HttpService
}

func NewFriendService() *friendService {
	return &friendService{}
}

func (s *friendService) ServiceName() string {
	return "friend"
}

func (s *friendService) OnModStart(ctx context.Context) error {
	Host := gxyservice.ServiceApp().Host
	svr := gxyhttp.HttpSystem().NewHttpServer(Host)
	gxyhttp.SetHandler(svr, ctx, "friend", &fl.FriendHandler{})
	gxylog.Info(ctx, "friend server starting")
	if err := svr.Start(); err != nil {
		return err
	}
	s.Svr = svr
	return nil
}

func (s *friendService) OnModStop(ctx context.Context) error {
	gxylog.Info(ctx, "friend service stopping")
	return s.Svr.Shutdown()
}
