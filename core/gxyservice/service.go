package gxyservice

import (
	"gserver/core/gxymodule"

	"ergo.services/ergo/gen"
	"github.com/tochemey/goakt/v3/actor"
)

type WorkerCreator func() gen.ProcessBehavior

type IService interface {
	gxymodule.IModule
	Name() string
	Worker() actor.Actor
	IsRemote() bool
}
