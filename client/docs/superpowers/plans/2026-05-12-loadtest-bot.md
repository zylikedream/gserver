# Load Test Bot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `cmd/bench/` — a config-driven load testing program that runs N concurrent bot connections against gserver.

**Architecture:** New standalone program in `cmd/bench/` reusing existing `pkg/client/` SDK. Shared field-parsing logic extracted from `cmd/hy/autocmd.go` into `pkg/client/fieldparse.go`. YAML config defines bot count, account pattern, scenario script (list of proto messages + fields + delays), and report interval. Each bot runs in its own goroutine with an independent `client.Client`.

**Tech Stack:** Go, protobuf/protoreflect, `gopkg.in/yaml.v3`

---

### Task 1: Move field parsing to pkg/client/fieldparse.go

**Files:**
- Create: `pkg/client/fieldparse.go`
- Modify: none yet (imports updated in Task 2)

- [ ] **Step 1: Create fieldparse.go with shared field parsing**

```go
package client

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

type FieldInfo struct {
	Name     string
	Kind     protoreflect.Kind
	Repeated bool
}

func ParseFieldValue(s string, fi FieldInfo) (protoreflect.Value, error) {
	switch fi.Kind {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(s), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfInt32(int32(n)), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfInt64(n), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfUint32(uint32(n)), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfUint64(n), nil
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(s == "1" || strings.EqualFold(s, "true")), nil
	case protoreflect.EnumKind:
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("enum value must be numeric: %s", s)
		}
		return protoreflect.ValueOfEnum(protoreflect.EnumNumber(n)), nil
	default:
		return protoreflect.Value{}, fmt.Errorf("unsupported kind %v", fi.Kind)
	}
}

func ExpandCommaSeparated(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.Contains(a, ",") {
			for _, p := range strings.Split(a, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
		} else {
			out = append(out, a)
		}
	}
	return out
}
```

- [ ] **Step 2: Write tests for the extracted functions**

Create `pkg/client/fieldparse_test.go`:

```go
package client

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestParseFieldValueString(t *testing.T) {
	v, err := ParseFieldValue("hello", FieldInfo{Kind: protoreflect.StringKind})
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "hello" {
		t.Errorf("got %q, want %q", v.String(), "hello")
	}
}

func TestParseFieldValueInt32(t *testing.T) {
	v, err := ParseFieldValue("42", FieldInfo{Kind: protoreflect.Int32Kind})
	if err != nil {
		t.Fatal(err)
	}
	if v.Int() != 42 {
		t.Errorf("got %d, want %d", v.Int(), 42)
	}
}

func TestParseFieldValueInt64(t *testing.T) {
	v, err := ParseFieldValue("1234567890123", FieldInfo{Kind: protoreflect.Int64Kind})
	if err != nil {
		t.Fatal(err)
	}
	if v.Int() != 1234567890123 {
		t.Errorf("got %d, want %d", v.Int(), 1234567890123)
	}
}

func TestParseFieldValueBool(t *testing.T) {
	for _, s := range []string{"1", "true", "True", "TRUE"} {
		v, err := ParseFieldValue(s, FieldInfo{Kind: protoreflect.BoolKind})
		if err != nil {
			t.Fatalf("ParseFieldValue(%q): %v", s, err)
		}
		if !v.Bool() {
			t.Errorf("ParseFieldValue(%q) = false, want true", s)
		}
	}
	v, err := ParseFieldValue("0", FieldInfo{Kind: protoreflect.BoolKind})
	if err != nil {
		t.Fatal(err)
	}
	if v.Bool() {
		t.Error("ParseFieldValue(\"0\") = true, want false")
	}
}

func TestParseFieldValueEnum(t *testing.T) {
	v, err := ParseFieldValue("2", FieldInfo{Kind: protoreflect.EnumKind})
	if err != nil {
		t.Fatal(err)
	}
	if v.Enum() != 2 {
		t.Errorf("got %d, want %d", v.Enum(), 2)
	}
}

func TestParseFieldValueUnsupported(t *testing.T) {
	_, err := ParseFieldValue("x", FieldInfo{Kind: protoreflect.MessageKind})
	if err == nil {
		t.Error("expected error for MessageKind")
	}
}

func TestExpandCommaSeparated(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{[]string{"a", "b"}, []string{"a", "b"}},
		{[]string{"a,b", "c"}, []string{"a", "b", "c"}},
		{[]string{"1,2,3"}, []string{"1", "2", "3"}},
		{[]string{""}, []string{}},
		{[]string{}, []string{}},
	}
	for _, tc := range tests {
		got := ExpandCommaSeparated(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("ExpandCommaSeparated(%v) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ExpandCommaSeparated(%v) = %v, want %v", tc.input, got, tc.want)
				break
			}
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they pass**

Run: `go test ./pkg/client/ -v -run TestParseFieldValue -run TestExpandCommaSeparated`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/client/fieldparse.go pkg/client/fieldparse_test.go
git commit -m "refactor: extract field parsing to pkg/client/fieldparse.go"
```

---

### Task 2: Update autocmd.go to use fieldparse.go

**Files:**
- Modify: `cmd/hy/autocmd.go`

- [ ] **Step 1: Remove fieldInfo, parseFieldValue, expandCommaSeparated from autocmd.go**

Replace the `fieldInfo` struct with an import alias to `client.FieldInfo`, and replace local `parseFieldValue`/`expandCommaSeparated` calls with `client.ParseFieldValue`/`client.ExpandCommaSeparated`.

Edit `cmd/hy/autocmd.go`:

Remove the `fieldInfo` struct (lines 15-19):
```go
type fieldInfo struct {
	Name     string
	Kind     protoreflect.Kind
	Repeated bool
}
```

Replace all uses of `fieldInfo` with `client.FieldInfo` inside `parseableFields()`:
```go
fi := client.FieldInfo{
```

Replace `parseFieldValue(args[argIdx], fi)` with `client.ParseFieldValue(args[argIdx], fi)` inside `buildAutoExec()`.

Replace `expandCommaSeparated(args[argIdx:])` with `client.ExpandCommaSeparated(args[argIdx:])` inside `buildAutoExec()`.

Remove the entire `parseFieldValue` function (lines 193-232).

Remove the entire `expandCommaSeparated` function (lines 234-249).

Remove `"strconv"` and `"strings"` from the import block in autocmd.go if they become unused after removing those functions (keep `"fmt"`, `"reflect"`, `"unicode"`).

- [ ] **Step 2: Build to verify**

Run: `go build ./cmd/hy/`
Expected: No errors

- [ ] **Step 3: Run all tests**

Run: `go test ./cmd/hy/ -v`
Expected: All tests PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/hy/autocmd.go
git commit -m "refactor: use pkg/client/fieldparse.go for field parsing"
```

---

### Task 3: Add yaml.v3 dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add gopkg.in/yaml.v3**

Run: `go get gopkg.in/yaml.v3`

- [ ] **Step 2: Verify the module builds**

Run: `go build ./...`
Expected: No errors, yaml.v3 is in go.mod

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "dep: add gopkg.in/yaml.v3"
```

---

### Task 4: Create cmd/bench/config.go

**Files:**
- Create: `cmd/bench/config.go`

- [ ] **Step 1: Write config parsing**

```go
package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Addr            string        `yaml:"addr"`
	Bots            int           `yaml:"bots"`
	AccountPattern  string        `yaml:"account_pattern"`
	Scenario        []Action      `yaml:"scenario"`
	ReportInterval  time.Duration `yaml:"report_interval"`
}

type Action struct {
	Msg    string                 `yaml:"msg"`
	Fields map[string]interface{} `yaml:"fields,omitempty"`
	Delay  DurationRange          `yaml:"delay"`
}

type DurationRange struct {
	Min time.Duration
	Max time.Duration
}

func (d *DurationRange) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	// Parse "5s" or "3-8s"
	if dur, err := time.ParseDuration(s); err == nil {
		d.Min = dur
		d.Max = dur
		return nil
	}
	if _, err := fmt.Sscanf(s, "%d-%d", &d.Min, &d.Max); err == nil {
		return nil
	}
	// Try with s/m/h suffix on each part
	var minStr, maxStr string
	if n, _ := fmt.Sscanf(s, "%s-%s", &minStr, &maxStr); n == 2 {
		min, err := time.ParseDuration(minStr)
		if err != nil {
			return fmt.Errorf("invalid delay %q: %v", s, err)
		}
		max, err := time.ParseDuration(maxStr)
		if err != nil {
			return fmt.Errorf("invalid delay %q: %v", s, err)
		}
		d.Min = min
		d.Max = max
		return nil
	}
	return fmt.Errorf("invalid delay %q: expected duration like \"5s\" or range like \"3-8s\"", s)
}

func (d DurationRange) Random() time.Duration {
	if d.Min >= d.Max {
		return d.Min
	}
	return d.Min + time.Duration(rand.Int63n(int64(d.Max-d.Min+1)))
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %v", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %v", err)
	}
	if cfg.Addr == "" {
		return nil, fmt.Errorf("addr is required")
	}
	if cfg.Bots <= 0 {
		return nil, fmt.Errorf("bots must be > 0")
	}
	if cfg.AccountPattern == "" {
		return nil, fmt.Errorf("account_pattern is required")
	}
	if len(cfg.Scenario) == 0 {
		return nil, fmt.Errorf("scenario is required")
	}
	if cfg.ReportInterval <= 0 {
		cfg.ReportInterval = 5 * time.Second
	}
	return &cfg, nil
}
```

- [ ] **Step 2: Build to verify**

Run: `go build ./cmd/bench/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add cmd/bench/config.go
git commit -m "feat(bench): config loading with YAML"
```

---

### Task 5: Create cmd/bench/metrics.go

**Files:**
- Create: `cmd/bench/metrics.go`

- [ ] **Step 1: Write metrics collection and reporter**

```go
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type BotState int32

const (
	BotConnecting BotState = iota
	BotAlive
	BotDead
)

type MetricsCollector struct {
	mu      sync.Mutex
	records map[string]*msgMetrics

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
		records: make(map[string]*msgMetrics),
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
	_ = now
}

func (m *MetricsCollector) PrintFinal() {
	fmt.Println("\n=== Final Report ===")
	m.Report(time.Now())
}
```

- [ ] **Step 2: Build to verify**

Run: `go build ./cmd/bench/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add cmd/bench/metrics.go
git commit -m "feat(bench): metrics collection and reporter"
```

---

### Task 6: Create cmd/bench/bot.go

**Files:**
- Create: `cmd/bench/bot.go`

- [ ] **Step 1: Write single bot logic**

```go
package main

import (
	"fmt"
	"math/rand"
	"time"

	"hy_client/pkg/client"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Bot struct {
	index   int
	uid     string
	cfg     *Config
	metrics *MetricsCollector
	cl      *client.Client
}

func NewBot(index int, uid string, cfg *Config, metrics *MetricsCollector) *Bot {
	return &Bot{
		index:   index,
		uid:     uid,
		cfg:     cfg,
		metrics: metrics,
	}
}

func (b *Bot) Run() {
	b.metrics.total.Add(1)
	b.metrics.alive.Add(1)
	defer b.metrics.alive.Add(-1)

	// Connect
	b.cl = client.NewClient(client.Config{
		Addr:       b.cfg.Addr,
		AccountUID: b.uid,
	})
	if err := b.cl.Connect(); err != nil {
		b.metrics.Record("Connect", 0, false)
		b.metrics.Record("Login", 0, false)
		return
	}
	defer b.cl.Close()

	// Login
	if err := b.cl.Handshake(); err != nil {
		b.metrics.Record("Handshake", 0, false)
		return
	}
	if err := b.cl.Login(); err != nil {
		b.metrics.Record("Login", 0, false)
		return
	}

	// Script loop
	for {
		for _, action := range b.cfg.Scenario {
			b.executeAction(action)
			delay := action.Delay.Random()
			if delay > 0 {
				time.Sleep(delay)
			}
		}
	}
}

func (b *Bot) executeAction(a Action) {
	// Build proto message from msg name
	msg := client.NewMessageByIDFromName(a.Msg)
	if msg == nil {
		// Try to find by name in registry
		return // skip unknown messages
	}
	mr := msg.ProtoReflect()
	md := mr.Descriptor()

	// Fill fields
	for name, rawVal := range a.Fields {
		fd := md.Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			continue
		}
		if fd.IsList() {
			list := mr.Mutable(fd).List()
			vals := yamlValuesToList(rawVal)
			for _, v := range vals {
				pv, err := yamlValueToProto(v, fd)
				if err != nil {
					continue
				}
				list.Append(pv)
			}
		} else {
			pv, err := yamlValueToProto(rawVal, fd)
			if err != nil {
				continue
			}
			mr.Set(fd, pv)
		}
	}

	// Send request and measure
	start := time.Now()
	err := b.cl.Request(msg)
	lat := time.Since(start)

	msgName := string(md.FullName())
	if err != nil {
		b.metrics.Record(msgName, lat, false)
	} else {
		b.metrics.Record(msgName, lat, true)
	}
}

func yamlValuesToList(v interface{}) []interface{} {
	switch val := v.(type) {
	case []interface{}:
		return val
	default:
		return []interface{}{v}
	}
}

func yamlValueToProto(v interface{}, fd protoreflect.FieldDescriptor) (protoreflect.Value, error) {
	// Convert YAML value to string, then use the shared parse function
	s := yamlValueToString(v)
	return client.ParseFieldValue(s, client.FieldInfo{
		Name: string(fd.Name()),
		Kind: fd.Kind(),
	})
}

func yamlValueToString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case float64:
		return fmt.Sprintf("%.0f", val)
	case bool:
		if val {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(v)
	}
}
```

- [ ] **Step 2: Check if NewMessageByIDFromName exists in registry.go**

Looking at registry.go, we have `NewMessageByID(id string)` but not a name-based lookup. Check if we need to add one.

Read `pkg/client/registry.go` to confirm.

If `MessageTypeByName(name string)` doesn't exist, add a helper in `pkg/client/registry.go`:

```go
func NewMessageByName(name string) proto.Message {
	// Look up message by full name (e.g. "galaxy.protocol.ReqBasicInfo")
	mt, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName("galaxy.protocol." + name))
	if err != nil {
		return nil
	}
	return mt.New().Interface()
}
```

Update bot.go to use `client.NewMessageByName` instead of `client.NewMessageByIDFromName`.

- [ ] **Step 3: Build to verify**

Run: `go build ./cmd/bench/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add cmd/bench/bot.go
git commit -m "feat(bench): single bot connect/login/script loop"
```

---

### Task 7: Create cmd/bench/manager.go

**Files:**
- Create: `cmd/bench/manager.go`

- [ ] **Step 1: Write BotManager**

```go
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
```

- [ ] **Step 2: Build to verify**

Run: `go build ./cmd/bench/`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add cmd/bench/manager.go
git commit -m "feat(bench): BotManager with signal handling and reporter"
```

---

### Task 8: Create cmd/bench/main.go

**Files:**
- Create: `cmd/bench/main.go`

- [ ] **Step 1: Write entry point**

```go
package main

import (
	"flag"
	"fmt"
	"os"

	"hy_client/pkg/client"
)

func main() {
	configPath := flag.String("config", "bench.yaml", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	client.RegisterMessages()

	mgr := NewBotManager(cfg)
	mgr.Run()
}
```

- [ ] **Step 2: Create a sample config for reference**

Create `cmd/bench/bench.yaml`:

```yaml
addr: "127.0.0.1:9000"
bots: 10
account_pattern: "loadtest_%d"
scenario:
  - msg: ReqBasicInfo
    delay: 5s
  - msg: ReqFriendList
    delay: 10s
  - msg: ReqChatSendChannel
    fields:
      channel_type: 1
      content: "hello everyone"
    delay: 3-8s
  - msg: ReqFlowerInfo
    delay: 30s
report_interval: 5s
```

- [ ] **Step 3: Build to verify**

Run: `go build ./cmd/bench/`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add cmd/bench/main.go cmd/bench/bench.yaml
git commit -m "feat(bench): entry point and sample config"
```

---

### Task 9: Build and verify everything

**Files:** none

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: No errors

- [ ] **Step 2: Run all tests**

Run: `go test ./...`
Expected: All tests PASS (pkg/client, cmd/hy, cmd/bench)

- [ ] **Step 3: Verify bench binary runs**

Run: `./bin/bench -h 2>&1 || ./cmd/bench/bench -h 2>&1`
Expected: Usage output showing `-config` flag

If `go build ./cmd/bench/` outputs to current dir, use: `./bench -h 2>&1`
If using `go run`: `go run ./cmd/bench/ -h 2>&1`

Expected output:
```
Usage of bench:
  -config string
    	path to config file (default "bench.yaml")
```

- [ ] **Step 4: Final commit**

```bash
git add -A
git commit -m "chore: finalize bench build and tests"
```
