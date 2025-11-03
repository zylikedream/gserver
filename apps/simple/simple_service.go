package simple

import (
	"context"
	"gserver/core/gxyactor"
	"gserver/service"

	"github.com/gogf/gf/v2/util/gconv"
)

type simpleActorService struct {
	gxyactor.ActorService
}

var simpleSvc = newSimpleService()

func SimpleService() *simpleActorService {
	return simpleSvc
}

func newSimpleService() *simpleActorService {
	return &simpleActorService{}
}

func (r *simpleActorService) Name() string {
	return service.SIMPLE_SERVICE
}

func (r *simpleActorService) Weight() int {
	return 0
}

func (r *simpleActorService) OnModStart(ctx context.Context) error {
	gxyactor.ActorSystem().RegisterGrain(r.Name(), func() gxyactor.IGrain {
		s := &SimpleGrain{}
		s.GrainBase = gxyactor.NewGrainBase(ctx, s)
		return s
	})
	return nil
}

func (s *simpleActorService) GetSimpleGrain(id int64) (gxyactor.PID, error) {
	pid, err := gxyactor.ActorSystem().GetGrain(s.Name(), gconv.String(id))
	if err != nil {
		return nil, err
	}
	return pid, nil
}

type SimpleGrain struct {
	*gxyactor.GrainBase
}

func (s *SimpleGrain) Init(ctx context.Context) error {
	return nil
}

func (s *SimpleGrain) HandleMessage(ctx context.Context, _ any) error {
	return nil
}

func (s *SimpleGrain) Terminate(ctx context.Context, _ error) {
}
