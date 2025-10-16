package simple

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/service"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/util/gconv"
)

type simpleService struct {
	gxyactor.Service
}

var simpleSvc = newSimpleService()

func SimpleService() *simpleService {
	return simpleSvc
}

func newSimpleService() *simpleService {
	return &simpleService{}
}

func (r *simpleService) Name() string {
	return service.SIMPLE_SERVICE
}

func (r *simpleService) Weight() int {
	return 0
}

func (r *simpleService) OnStart(ctx context.Context) error {
	gxyactor.ActorSystem().RegisterGrain(r.Name(), func() actor.Actor {
		return &SimpleGrain{}
	})
	return nil
}

func (s *simpleService) GetSimpleGrain(id int64) (gxyactor.PID, error) {
	pid, err := gxyactor.ActorSystem().GetGrain(s.Name(), gconv.String(id))
	if err != nil {
		return nil, err
	}
	return pid, nil
}

type SimpleGrain struct {
}

func (s *SimpleGrain) Receive(ctx actor.Context) {

}
