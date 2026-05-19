package main

import (
	"hy_client/pkg/client"
)

type Bot struct {
	index      int
	uid        string
	cfg        *Config
	botTypeCfg *BotTypeConfig
	metrics    *MetricsCollector
	cl         *client.Client
	state      *BotState
	log        *BotLogger
	stopCh     chan struct{}
}

func NewBot(index int, uid string, cfg *Config, botTypeCfg *BotTypeConfig, metrics *MetricsCollector) *Bot {
	return &Bot{
		index:      index,
		uid:        uid,
		cfg:        cfg,
		botTypeCfg: botTypeCfg,
		metrics:    metrics,
		stopCh:     make(chan struct{}),
	}
}

func (b *Bot) Stop() {
	close(b.stopCh)
	if b.cl != nil {
		b.cl.Close()
	}
}

func (b *Bot) Run() {
	b.metrics.total.Add(1)
	b.metrics.alive.Add(1)
	defer b.metrics.alive.Add(-1)

	b.cl = client.NewClient(client.Config{
		Addr:       b.cfg.Addr,
		AccountUID: b.uid,
	})
	defer b.cl.Close()

	b.state = NewBotState()
	b.log = NewBotLogger(b.index, b.botTypeCfg.ID)

	actions := NewBotActions(b.cl, b.state, b.log)
	actions.RegisterOnMessage()

	runner := NewScriptRunner(actions, b.cl, b.state, b.index, b.botTypeCfg.ID, b.log, b.cfg.ChatMixin)

	done := make(chan error, 1)
	go func() {
		done <- runner.RunScript(b.botTypeCfg.Script)
	}()

	select {
	case <-b.stopCh:
		return
	case err := <-done:
		if err != nil {
			b.log.Printf("bot stopped: %v", err)
		}
	}
}
