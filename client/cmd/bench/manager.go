package main

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type BotManager struct {
	cfg     *Config
	metrics *MetricsCollector
	bots    []*Bot
}

func NewBotManager(cfg *Config) *BotManager {
	return &BotManager{
		cfg:     cfg,
		metrics: NewMetricsCollector(),
	}
}

func (m *BotManager) Run() {
	fmt.Printf("Starting %d bots against %s\n", m.cfg.Bots, m.cfg.Addr)
	fmt.Printf("Scenario: %d actions\n", len(m.cfg.Scenario))

	// Start reporter
	stopReporter := make(chan struct{})
	go m.reportLoop(stopReporter)

	// Start bots
	var wg sync.WaitGroup
	for i := 0; i < m.cfg.Bots; i++ {
		uid := fmt.Sprintf(m.cfg.AccountPattern, i)
		bot := NewBot(i, uid, m.cfg, m.metrics)
		m.bots = append(m.bots, bot)
		wg.Add(1)
		go func() {
			defer wg.Done()
			bot.Run()
		}()
	}

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
	close(stopReporter)

	for _, bot := range m.bots {
		bot.Stop()
	}
	wg.Wait()
	m.metrics.PrintFinal()
}

func (m *BotManager) reportLoop(stop chan struct{}) {
	ticker := time.NewTicker(m.cfg.ReportInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.metrics.Report(time.Now())
		case <-stop:
			return
		}
	}
}
