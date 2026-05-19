package main

import (
	"fmt"
	"math/rand"
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
	fmt.Printf("Starting %d bots against %s\n", m.cfg.TotalBots, m.cfg.Addr)
	for _, bt := range m.cfg.BotTypes {
		fmt.Printf("  %s: %d%%\n", bt.ID, bt.Weight)
	}

	stopReporter := make(chan struct{})
	go m.reportLoop(stopReporter)

	var wg sync.WaitGroup
	startTime := time.Now()

	if m.cfg.StartupRate <= 0 {
		for i := 0; i < m.cfg.TotalBots; i++ {
			m.startBot(i, &wg)
		}
	} else {
		ticker := time.NewTicker(time.Second / time.Duration(m.cfg.StartupRate))
		defer ticker.Stop()

		for i := 0; i < m.cfg.TotalBots; i++ {
			<-ticker.C
			m.startBot(i, &wg)
		}
	}

	elapsed := time.Since(startTime)
	fmt.Printf("All bots launched in %v\n", elapsed)

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

func (m *BotManager) startBot(i int, wg *sync.WaitGroup) {
	uid := fmt.Sprintf(m.cfg.AccountPattern, i)
	botType := m.cfg.BotTypeFor(i)
	bot := NewBot(i, uid, m.cfg, botType, m.metrics)
	m.bots = append(m.bots, bot)

	jitter := time.Duration(rand.Int63n(500)) * time.Millisecond
	time.Sleep(jitter)

	wg.Add(1)
	go func() {
		defer wg.Done()
		bot.Run()
	}()
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
