package social

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/core/gxyhttp"
	"gserver/core/gxymq"
	"gserver/protocol/pb"
	"gserver/service"
	"gserver/service/social/api"
	"gserver/service/social/internal/logic"

	"github.com/gogf/gf/v2/util/gconv"
)

type socialService struct {
	mq *gxymq.MessageQueue
	gxyhttp.HttpService
}

var socialSvc = newSocialService()

func newSocialService() *socialService {
	return &socialService{}
}

func SocialService() *socialService {
	return socialSvc
}

func (s *socialService) Name() string {
	return service.SOCIAL_SERVICE
}

func (s *socialService) OnModStart(ctx context.Context) error {
	s.mq.Subscribe(ctx, api.TOPIC_SOCIAL_FRIEND_NOTIFY, func(ctx context.Context, msg string) error {
		return nil
	})
	if err := s.mq.Start(ctx); err != nil {
		return err
	}
	return nil
}

func (s *socialService) OnModInit(ctx context.Context) error {
	mq, err := gxymq.NewMessageQueue(gxymq.MQTypeRedis, "node/config/service.toml")
	if err != nil {
		return err
	}
	s.mq = mq
	s.SetHandler(ctx, s.Name(), logic.NewSocialController())
	return nil
}

func (s *socialService) RoleOnline(ctx context.Context, roleID int64, pid gxyactor.PID) (*api.RoleOnlineRes, error) {
	req := &api.RoleOnlineReq{
		RoleID: roleID,
		Pid: pb.ActorPid{
			Address: pid.Address,
			Id:      pid.Id,
		},
	}
	uri := s.GetReqUri(req)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, s.Name(), uri, req)
	if err != nil {
		return nil, err
	}

	res := &api.RoleOnlineRes{}
	if err := gconv.Struct(rsp.Data, res); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *socialService) RoleOffline(ctx context.Context, roleID int64) (*api.RoleOfflineRes, error) {
	req := &api.RoleOfflineReq{
		RoleID: roleID,
	}
	uri := s.GetReqUri(req)
	rsp, err := gxyhttp.HttpSystem().PostService(ctx, s.Name(), uri, req)
	if err != nil {
		return nil, err
	}

	res := &api.RoleOfflineRes{}
	if err := gconv.Struct(rsp.Data, res); err != nil {
		return nil, err
	}
	return res, nil
}
