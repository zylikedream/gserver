package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// BotLogger is a simple logger for bot actions with bot identity tracking.
type BotLogger struct {
	botID   int
	botType string
}

func NewBotLogger(botID int, botType string) *BotLogger {
	return &BotLogger{botID: botID, botType: botType}
}

func (l *BotLogger) Printf(format string, args ...interface{}) {
	fmt.Printf("[bot=%d type=%s] %s\n", l.botID, l.botType, fmt.Sprintf(format, args...))
}

type BotConnState int32

const (
	BotConnecting BotConnState = iota
	BotAlive
	BotDead
)

type MetricsCollector struct {
	mu      sync.Mutex
	records map[string]*msgMetrics

	typeMetrics map[string]*msgMetrics

	alive atomic.Int32
	total atomic.Int32
}

type msgMetrics struct {
	count    atomic.Int64
	fail     atomic.Int64
	totalLat atomic.Int64
	maxLat   atomic.Int64
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		records:     make(map[string]*msgMetrics),
		typeMetrics: make(map[string]*msgMetrics),
	}
}

func (m *MetricsCollector) Record(msgName string, lat time.Duration, success bool) {
	m.mu.Lock()
	r, ok := m.records[msgName]
	if !ok {
		r = &msgMetrics{}
		m.records[msgName] = r
	}
	m.mu.Unlock()

	r.count.Add(1)
	r.totalLat.Add(int64(lat))
	if lat > time.Duration(r.maxLat.Load()) {
		r.maxLat.Store(int64(lat))
	}
	if !success {
		r.fail.Add(1)
	}
}

func (m *MetricsCollector) RecordType(botType, action string, lat time.Duration, success bool) {
	m.mu.Lock()
	r, ok := m.typeMetrics[botType]
	if !ok {
		r = &msgMetrics{}
		m.typeMetrics[botType] = r
	}
	m.mu.Unlock()

	r.count.Add(1)
	r.totalLat.Add(int64(lat))
	if lat > time.Duration(r.maxLat.Load()) {
		r.maxLat.Store(int64(lat))
	}
	if !success {
		r.fail.Add(1)
	}
}

func (m *MetricsCollector) Report(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	alive := m.alive.Load()
	total := m.total.Load()
	fmt.Printf("\n[Bots]  alive: %d/%d  dead: %d\n", alive, total, total-alive)
	for name, r := range m.records {
		count := r.count.Load()
		if count == 0 {
			continue
		}
		fail := r.fail.Load()
		maxLat := time.Duration(r.maxLat.Load())
		avgLat := time.Duration(r.totalLat.Load() / count)
		pct := 100.0
		if fail > 0 {
			pct = 100.0 * float64(count-fail) / float64(count)
		}
		fmt.Printf("[%s]  %6d  avg: %v  max: %v  ok: %.1f%%\n", name, count, avgLat, maxLat, pct)
	}

	// Per-bot-type summary
	if len(m.typeMetrics) > 0 {
		fmt.Println("--- By Bot Type ---")
		for name, r := range m.typeMetrics {
			count := r.count.Load()
			if count == 0 {
				continue
			}
			fail := r.fail.Load()
			avgLat := time.Duration(r.totalLat.Load() / count)
			maxLat := time.Duration(r.maxLat.Load())
			pct := 100.0
			if fail > 0 {
				pct = 100.0 * float64(count-fail) / float64(count)
			}
			fmt.Printf("[%s]  avg: %v  max: %v  ok: %.1f%%\n", name, avgLat, maxLat, pct)
		}
	}
	_ = now
}

func (m *MetricsCollector) PrintFinal() {
	fmt.Println("\n=== Final Report ===")
	m.Report(time.Now())
}
