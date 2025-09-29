package gxyservice

import (
	"gserver/core/gxymodule"

	"ergo.services/ergo/gen"
)

type WorkerCreator func() gen.ProcessBehavior

type IService interface {
	gxymodule.IModule
	Name() string
}
