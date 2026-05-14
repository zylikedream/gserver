package main

import (
	"fmt"
	"time"

	"hy_client/pkg/client"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Bot struct {
	index   int
	uid     string
	cfg     *Config
	metrics *MetricsCollector
	cl      *client.Client
	stopCh  chan struct{}
}

func NewBot(index int, uid string, cfg *Config, metrics *MetricsCollector) *Bot {
	return &Bot{
		index:   index,
		uid:     uid,
		cfg:     cfg,
		metrics: metrics,
		stopCh:  make(chan struct{}),
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

	// Connect
	b.cl = client.NewClient(client.Config{
		Addr:       b.cfg.Addr,
		AccountUID: b.uid,
	})
	if err := b.cl.Connect(); err != nil {
		fmt.Printf("[Bot %d] Connect failed: %v\n", b.index, err)
		b.metrics.Record("Connect", 0, false)
		return
	}
	defer b.cl.Close()

	// Login
	if _, err := b.cl.Handshake(); err != nil {
		fmt.Printf("[Bot %d] Handshake failed: %v\n", b.index, err)
		b.metrics.Record("Handshake", 0, false)
		return
	}
	if _, err := b.cl.Login(); err != nil {
		fmt.Printf("[Bot %d] Login failed: %v\n", b.index, err)
		b.metrics.Record("Login", 0, false)
		return
	}

	// Script loop
	for {
		for _, action := range b.cfg.Scenario {
			select {
			case <-b.stopCh:
				return
			default:
			}
			b.executeAction(action)
			delay := action.Delay.Random()
			if delay > 0 {
				select {
				case <-b.stopCh:
					return
				case <-time.After(delay):
				}
			}
		}
	}
}

func (b *Bot) executeAction(a Action) {
	msg := client.NewMessageByName(a.Msg)
	if msg == nil {
		return
	}
	protoReflect := msg.ProtoReflect()
	md := protoReflect.Descriptor()

	for name, rawVal := range a.Fields {
		fd := md.Fields().ByName(protoreflect.Name(name))
		if fd == nil {
			continue
		}
		if fd.IsList() {
			list := protoReflect.Mutable(fd).List()
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
			protoReflect.Set(fd, pv)
		}
	}

	start := time.Now()
	err := b.cl.Request(msg)
	lat := time.Since(start)

	fullName := string(md.FullName())
	if err != nil {
		fmt.Printf("[Bot %d] %s failed: %v\n", b.index, fullName, err)
		b.metrics.Record(fullName, lat, false)
	} else {
		b.metrics.Record(fullName, lat, true)
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
