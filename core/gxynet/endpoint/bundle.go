package endpoint

import (
	"gserver/core/gxynet/processor"

	"github.com/gogf/gf/v2/os/gcfg"
)

type CoreBundle struct {
	processor.Processor
	Handler EventHandler
}

func (p *CoreBundle) BindProc(c *gcfg.Config) error {
	proc, err := processor.NewProcessor(c)
	if err != nil {
		return err
	}
	p.Processor = proc
	return nil
}

func (p *CoreBundle) BindHandler(handler EventHandler) {
	p.Handler = handler
}
