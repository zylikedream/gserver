package main

import (
	"fmt"
	"sync"

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

	gateAddr := b.cfg.Addr
	gateToken := ""

	if b.cfg.AccountServer != "" {
		platform := b.cfg.Platform
		if platform == "" {
			platform = "guest"
		}
		preloginData, err := client.AccountServerPrelogin(b.cfg.AccountServer, platform, b.uid, "")
		if err != nil {
			b.log.Printf("prelogin failed: %v", err)
			return
		}
		gateAddr = fmt.Sprintf("%s:%d", preloginData.Gate.Host, preloginData.Gate.Port)
		gateToken = preloginData.GateToken
	}

	b.cl = client.NewClient(client.Config{
		Addr: gateAddr,
	})
	defer b.cl.Close()

	disconnected := make(chan struct{})
	var once sync.Once
	b.cl.OnDisconnect(func(reason error) {
		once.Do(func() { close(disconnected) })
	})

	b.state = NewBotState()
	b.state.GateToken = gateToken
	b.log = NewBotLogger(b.index, b.botTypeCfg.ID)

	actions := NewBotActions(b.cl, b.state, b.log)
	actions.RegisterOnMessage()

	runner := NewScriptRunner(actions, b.cl, b.state, b.index, b.botTypeCfg.ID, b.log, b.cfg.ChatMixin, b.cfg.Silent, b.stopCh)

	done := make(chan error, 1)
	go func() {
		done <- runner.RunScript(b.botTypeCfg.Script)
	}()

	select {
	case <-b.stopCh:
		return
	case <-disconnected:
		b.log.Printf("server disconnected")
		return
	case err := <-done:
		if err != nil {
			b.log.Printf("bot stopped: %v", err)
		}
	}
}
