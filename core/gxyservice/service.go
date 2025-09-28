package gxyservice

import (
	"gserver/core/gxymodule"

	"ergo.services/ergo/gen"
	"github.com/asynkron/protoactor-go/actor"
)

type WorkerCreator func() gen.ProcessBehavior

type IService interface {
	gxymodule.IModule
	Name() string
	Worker() actor.Actor
	IsRemote() bool
}
