package gxyservice

import "ergo.services/ergo/gen"

type WorkerCreator func() gen.ProcessBehavior

type IService interface {
	Name() string
	Worker() WorkerCreator
}
