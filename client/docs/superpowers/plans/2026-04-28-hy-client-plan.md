# hy_client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a console REPL client (hy) for the "My Garden World" game server, with a reusable core SDK (`pkg/client/`).

**Architecture:** Three-layer — LTIV codec + protobuf registry in SDK, TCP connection manager, REPL frontend. SDK is a standalone package with no frontend dependencies; cmd/hy imports it.

**Tech Stack:** Go 1.25, standard library `net`, protobuf (`google.golang.org/protobuf`), protoc/buf for code generation, TOML config (`BurntSushi/toml`).

**Spec:** `docs/superpowers/specs/2026-04-28-hy-client-design.md`

---

## File Map

| File | Responsibility |
|------|---------------|
| `go.mod` | Module definition |
| `Makefile` | build, proto, test targets |
| `config/default.toml` | Default server config |
| `CLAUDE.md` | Project instructions |
| `proto/client/*.proto` | Proto source files (copied from gserver, later git subtree) |
| `pb/*.pb.go` | Generated protobuf Go code |
| `pkg/client/codec.go` | LTIV packet encode/decode |
| `pkg/client/codec_test.go` | Codec unit tests |
| `pkg/client/registry.go` | msg_id ↔ proto type bidirectional registry |
| `pkg/client/registry_test.go` | Registry unit tests |
| `pkg/client/conn.go` | TCP connection, read loop, send |
| `pkg/client/conn_test.go` | Connection tests with mock server |
| `pkg/client/client.go` | High-level Client API (Connect, Handshake, Login, Send, Request) |
| `pkg/client/client_test.go` | Client integration tests |
| `cmd/hy/main.go` | REPL entry point, config parsing, login flow |
| `cmd/hy/repl.go` | REPL loop, command parsing |
| `cmd/hy/commands.go` | Command registry and all command definitions |

---

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `config/default.toml`
- Create: `CLAUDE.md`

- [ ] **Step 1: Initialize git repo and go module**

```bash
cd /home/zyr/workspace/hy_client
git init
go mod init hy_client
```

- [ ] **Step 2: Create Makefile**

Create `Makefile`:

```makefile
.PHONY: build proto test clean

build:
	go build -o bin/hy ./cmd/hy/

proto:
	protoc --go_out=pb --go_opt=paths=source_relative proto/client/*.proto

test:
	go test ./...

clean:
	rm -rf bin/ pb/
```

- [ ] **Step 3: Create default config**

Create `config/default.toml`:

```toml
[server]
addr = "127.0.0.1:9000"
account = "account001"
```

- [ ] **Step 4: Create CLAUDE.md**

Create `CLAUDE.md`:

```markdown
# hy_client

Console debugging client for "My Garden World" (gserver).

## Build & Run

```bash
make build          # build binary
make proto          # regenerate protobuf
make test           # run tests
./bin/hy            # run with defaults
./bin/hy --addr=127.0.0.1:9000 --account=account001
```

## Architecture

- `pkg/client/` — Core SDK: codec, registry, connection, client API
- `cmd/hy/` — Console REPL frontend
- `pb/` — Generated protobuf Go code
- `proto/client/` — Proto source files (from gserver protocol)

## Protocol

LTIV: `[Size:2B LE][Type:1B][ID:2B LE][Payload:protobuf]`
```

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: project scaffolding with go.mod, Makefile, config, CLAUDE.md"
```

---

### Task 2: Proto Files and Code Generation

**Files:**
- Create: `proto/client/msg_options.proto`
- Create: `proto/client/login.proto`
- Create: `proto/client/basic.proto`
- Create: `proto/client/bag.proto`
- Create: `proto/client/sign.proto`
- Create: `proto/client/role.proto`
- Create: `proto/client/friend.proto`
- Create: `proto/client/mahong.proto`
- Create: `pb/` (generated)

- [ ] **Step 1: Copy proto files from gserver**

```bash
mkdir -p /home/zyr/workspace/hy_client/proto/client
cp /home/zyr/workspace/gserver/protocol/client/*.proto /home/zyr/workspace/hy_client/proto/client/
```

- [ ] **Step 2: Verify proto files are copied**

```bash
ls /home/zyr/workspace/hy_client/proto/client/
```

Expected: `ack.proto bag.proto basic.proto friend.proto login.proto mahong.proto msg_options.proto role.proto sign.proto`

- [ ] **Step 3: Check role.proto exists (needed by friend.proto and mahong.proto)**

```bash
cat /home/zyr/workspace/hy_client/proto/client/role.proto
```

If it doesn't exist, check `/home/zyr/workspace/gserver/protocol/client/role.proto` for its content and copy it.

- [ ] **Step 4: Add protobuf dependency**

```bash
cd /home/zyr/workspace/hy_client
go get google.golang.org/protobuf
```

- [ ] **Step 5: Generate Go code**

```bash
cd /home/zyr/workspace/hy_client
mkdir -p pb
protoc --go_out=pb --go_opt=paths=source_relative proto/client/*.proto
```

- [ ] **Step 6: Verify generation**

```bash
ls /home/zyr/workspace/hy_client/pb/
```

Expected: `.pb.go` files for all proto files.

- [ ] **Step 7: Fix go_package if needed**

If protoc errors about `go_package`, edit each `.proto` file to set `option go_package = "hy_client/pb";`. Use this sed command:

```bash
cd /home/zyr/workspace/hy_client
sed -i 's|option go_package="./pb;pb";|option go_package = "hy_client/pb";|g' proto/client/*.proto
```

Then re-run `make proto` and verify.

- [ ] **Step 8: Verify generated code compiles**

```bash
cd /home/zyr/workspace/hy_client
go build ./pb/...
```

Expected: no errors.

- [ ] **Step 9: Commit**

```bash
git add proto/ pb/ go.mod go.sum
git commit -m "feat: add proto source files and generated Go code"
```

---

### Task 3: LTIV Codec

**Files:**
- Create: `pkg/client/codec.go`
- Create: `pkg/client/codec_test.go`

- [ ] **Step 1: Write failing test for codec encode**

Create `pkg/client/codec_test.go`:

```go
package client

import (
	"testing"
)

func TestEncodeDecodeHandshake(t *testing.T) {
	codec := NewLTIVCodec()

	// Encode a handshake message: Type=0 (first packet), Path="10001", Payload=proto bytes
	payload := []byte{0x0a, 0x09, 0x74, 0x65, 0x73, 0x74, 0x5f, 0x75, 0x69, 0x64} // field 1, "test_uid"
	msg := &Message{
		Type:    0, // first packet
		Path:    "10001",
		Payload: payload,
	}

	data, err := codec.Encode(msg)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	// Verify structure: [Size:2B][Type:1B][ID:2B][Payload]
	if len(data) < 5 {
		t.Fatalf("encoded data too short: %d bytes", len(data))
	}

	// Size should be 1 (type) + 2 (id) + len(payload)
	expectedSize := 1 + 2 + len(payload)
	actualSize := int(data[0]) | int(data[1])<<8
	if actualSize != expectedSize {
		t.Errorf("size mismatch: got %d, want %d", actualSize, expectedSize)
	}

	// Type should be 0
	if data[2] != 0 {
		t.Errorf("type mismatch: got %d, want 0", data[2])
	}

	// Decode back
	consumed, decoded, err := codec.Decode(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if consumed != len(data) {
		t.Errorf("consumed mismatch: got %d, want %d", consumed, len(data))
	}
	if decoded.Type != 0 {
		t.Errorf("decoded type mismatch: got %d, want 0", decoded.Type)
	}
	if decoded.Path != "10001" {
		t.Errorf("decoded path mismatch: got %s, want 10001", decoded.Path)
	}
}

func TestDecodePartialData(t *testing.T) {
	codec := NewLTIVCodec()

	// Too short to read size
	_, _, err := codec.Decode([]byte{0x01})
	if err != ErrHeadNotEnough {
		t.Errorf("expected ErrHeadNotEnough, got: %v", err)
	}

	// Size says 10 bytes but only 3 available
	_, _, err = codec.Decode([]byte{0x0a, 0x00, 0x01})
	if err != ErrBodyNotEnough {
		t.Errorf("expected ErrBodyNotEnough, got: %v", err)
	}
}

func TestEncodeDecodeDataPacket(t *testing.T) {
	codec := NewLTIVCodec()

	payload := []byte{0x01, 0x02, 0x03}
	msg := &Message{
		Type:    1, // data packet
		Path:    "21001",
		Payload: payload,
	}

	data, err := codec.Encode(msg)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	consumed, decoded, err := codec.Decode(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if consumed != len(data) {
		t.Errorf("consumed mismatch: got %d, want %d", consumed, len(data))
	}
	if decoded.Type != 1 {
		t.Errorf("type mismatch: got %d, want 1", decoded.Type)
	}
	if decoded.Path != "21001" {
		t.Errorf("path mismatch: got %s, want 21001", decoded.Path)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/zyr/workspace/hy_client
go test ./pkg/client/ -run TestEncode -v
```

Expected: compilation error (types not defined).

- [ ] **Step 3: Implement codec**

Create `pkg/client/codec.go`:

```go
package client

import (
	"encoding/binary"
	"errors"
)

const (
	sizeLen   = 2
	typeLen   = 1
	idLen     = 2
	maxSize   = 3 * 1024 * 1024 // 3MB
)

var (
	ErrHeadNotEnough = errors.New("packet header not enough")
	ErrBodyNotEnough = errors.New("packet body not enough")
	ErrPacketTooBig  = errors.New("packet too big")
)

type Message struct {
	Type    uint16
	Path    string
	Payload []byte
}

type LTIVCodec struct{}

func NewLTIVCodec() *LTIVCodec {
	return &LTIVCodec{}
}

func (c *LTIVCodec) Encode(msg *Message) ([]byte, error) {
	// body = Type(1B) + ID(2B) + Payload
	body := make([]byte, 0, typeLen+idLen+len(msg.Payload))
	body = append(body, byte(msg.Type))

	pathNum := uint16(0)
	for _, ch := range msg.Path {
		pathNum = pathNum*10 + uint16(ch-'0')
	}
	idBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(idBuf, pathNum)
	body = append(body, idBuf...)
	body = append(body, msg.Payload...)

	// packet = Size(2B) + body
	sizeBuf := make([]byte, 2)
	binary.LittleEndian.PutUint16(sizeBuf, uint16(len(body)))

	result := make([]byte, 0, sizeLen+len(body))
	result = append(result, sizeBuf...)
	result = append(result, body...)
	return result, nil
}

func (c *LTIVCodec) Decode(data []byte) (int, *Message, error) {
	if len(data) < sizeLen {
		return 0, nil, ErrHeadNotEnough
	}

	bodySize := int(binary.LittleEndian.Uint16(data[:sizeLen]))
	if bodySize > maxSize {
		return 0, nil, ErrPacketTooBig
	}

	if len(data)-sizeLen < bodySize {
		return 0, nil, ErrBodyNotEnough
	}

	body := data[sizeLen : sizeLen+bodySize]
	if len(body) < typeLen+idLen {
		return 0, nil, errors.New("body too short")
	}

	msgType := uint16(body[0])
	msgID := binary.LittleEndian.Uint16(body[typeLen : typeLen+idLen])
	payload := body[typeLen+idLen:]

	return sizeLen + bodySize, &Message{
		Type:    msgType,
		Path:    idToString(msgID),
		Payload: payload,
	}, nil
}

func idToString(id uint16) string {
	if id == 0 {
		return "0"
	}
	digits := make([]byte, 0, 5)
	for id > 0 {
		digits = append([]byte{byte('0' + id%10)}, digits...)
		id /= 10
	}
	return string(digits)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/zyr/workspace/hy_client
go test ./pkg/client/ -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/client/codec.go pkg/client/codec_test.go
git commit -m "feat: LTIV codec with encode/decode and tests"
```

---

### Task 4: Message Registry

**Files:**
- Create: `pkg/client/registry.go`
- Create: `pkg/client/registry_test.go`

- [ ] **Step 1: Write failing test for registry**

Create `pkg/client/registry_test.go`:

```go
package client

import (
	"testing"

	"hy_client/pb"
)

func TestRegistryRegistersAllMessages(t *testing.T) {
	RegisterMessages()

	// Test known message IDs
	testCases := []struct {
		id   string
		name string
	}{
		{"10001", "ReqHandShake"},
		{"10002", "RspHandShake"},
		{"10003", "ReqAccountLogin"},
		{"10004", "RspAccountLogin"},
		{"20001", "ReqBasicSetName"},
		{"21001", "ReqBagInfo"},
		{"22001", "ReqSignInfo"},
		{"23001", "ReqFriendInfo"},
		{"24001", "ReqMahongCreateRoom"},
	}

	for _, tc := range testCases {
		msgType := MessageTypeByID(tc.id)
		if msgType == nil {
			t.Errorf("no message registered for id %s", tc.id)
			continue
		}
		if msgType.Name() != tc.name {
			t.Errorf("id %s: got name %s, want %s", tc.id, msgType.Name(), tc.name)
		}
	}
}

func TestRegistryBidirectional(t *testing.T) {
	RegisterMessages()

	msg := &pb.ReqHandShake{}
	id := IDByMessage(msg)
	if id != "10001" {
		t.Errorf("ReqHandShake ID: got %s, want 10001", id)
	}
}

func TestRegistryNewInstance(t *testing.T) {
	RegisterMessages()

	msg := NewMessageByID("10001")
	if msg == nil {
		t.Fatal("expected non-nil message for id 10001")
	}
	handshake, ok := msg.(*pb.ReqHandShake)
	if !ok {
		t.Fatal("expected *pb.ReqHandShake")
	}
	handshake.AccountUid = "test"
	if handshake.AccountUid != "test" {
		t.Error("failed to set field on new instance")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/zyr/workspace/hy_client
go test ./pkg/client/ -run TestRegistry -v
```

Expected: compilation error (functions not defined).

- [ ] **Step 3: Implement registry**

Create `pkg/client/registry.go`:

```go
package client

import (
	"fmt"
	"reflect"
	"strconv"

	pb "hy_client/pb"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var (
	metaByID   = map[string]*messageMeta{}
	metaByType = map[reflect.Type]*messageMeta{}
)

type messageMeta struct {
	id   string
	name string
	typ  reflect.Type
}

var registered bool

func RegisterMessages() {
	if registered {
		return
	}
	registered = true

	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if fd.Package() != "galaxy.protocol" {
			return true
		}
		msgs := fd.Messages()
		for i := 0; i < msgs.Len(); i++ {
			registerMessage(msgs.Get(i))
		}
		return true
	})
}

func registerMessage(md protoreflect.MessageDescriptor) {
	opts := md.Options()
	if opts == nil {
		return
	}
	msgID := proto.GetExtension(opts, pb.E_MsgId).(uint32)
	if msgID == 0 {
		return
	}

	msgType, err := protoregistry.GlobalTypes.FindMessageByName(md.FullName())
	if err != nil {
		return
	}
	instance := msgType.New().Interface()

	id := strconv.FormatUint(uint64(msgID), 10)
	typ := reflect.TypeOf(instance)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	meta := &messageMeta{
		id:   id,
		name: string(md.Name()),
		typ:  typ,
	}
	metaByID[id] = meta
	metaByType[typ] = meta
}

func MessageTypeByID(id string) reflect.Type {
	m, ok := metaByID[id]
	if !ok {
		return nil
	}
	return m.typ
}

func IDByMessage(msg proto.Message) string {
	typ := reflect.TypeOf(msg)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	m, ok := metaByType[typ]
	if !ok {
		return ""
	}
	return m.id
}

func NewMessageByID(id string) proto.Message {
	m, ok := metaByID[id]
	if !ok {
		return nil
	}
	val := reflect.New(m.typ)
	msg, ok := val.Interface().(proto.Message)
	if !ok {
		return nil
	}
	return msg
}

func MessageNameByID(id string) string {
	m, ok := metaByID[id]
	if !ok {
		return fmt.Sprintf("Unknown(%s)", id)
	}
	return m.name
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/zyr/workspace/hy_client
go test ./pkg/client/ -run TestRegistry -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/client/registry.go pkg/client/registry_test.go
git commit -m "feat: message registry with msg_id bidirectional mapping"
```

---

### Task 5: TCP Connection

**Files:**
- Create: `pkg/client/conn.go`
- Create: `pkg/client/conn_test.go`

- [ ] **Step 1: Write failing test for connection send/receive**

Create `pkg/client/conn_test.go`:

```go
package client

import (
	"net"
	"testing"
	"time"

	pb "hy_client/pb"
)

func TestConnSendAndReceive(t *testing.T) {
	RegisterMessages()

	// Start mock server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverCh := make(chan *Message, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		codec := NewLTIVCodec()
		consumed, msg, err := codec.Decode(buf[:n])
		if err != nil {
			return
		}
		if consumed != n {
			t.Errorf("server: consumed %d != n %d", consumed, n)
		}
		serverCh <- msg

		// Echo back a response
		rsp := &Message{
			Type:    1,
			Path:    "10002",
			Payload: []byte{0x0a, 0x04, 0x74, 0x65, 0x73, 0x74}, // field 1 = "test"
		}
		data, _ := codec.Encode(rsp)
		conn.Write(data)
	}()

	// Connect client
	received := make(chan *Message, 1)
	conn := NewConn(func(msg *Message) {
		received <- msg
	})

	err = conn.Connect(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Send a message
	err = conn.SendRaw(&Message{
		Type:    0,
		Path:    "10001",
		Payload: []byte{0x0a, 0x04, 0x74, 0x65, 0x73, 0x74},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify server received
	select {
	case srvMsg := <-serverCh:
		if srvMsg.Path != "10001" {
			t.Errorf("server received path %s, want 10001", srvMsg.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for server to receive")
	}

	// Verify client received response
	select {
	case cliMsg := <-received:
		if cliMsg.Path != "10002" {
			t.Errorf("client received path %s, want 10002", cliMsg.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for client to receive")
	}
}

func TestConnSendProtobuf(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		conn.Read(buf)
	}()

	conn := NewConn(func(msg *Message) {})
	err = conn.Connect(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	err = conn.Send(&pb.ReqHandShake{AccountUid: "test_account"})
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/zyr/workspace/hy_client
go test ./pkg/client/ -run TestConn -v
```

Expected: compilation error.

- [ ] **Step 3: Implement connection**

Create `pkg/client/conn.go`:

```go
package client

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"

	"google.golang.org/protobuf/proto"
)

type MessageHandler func(msg *Message)

type Conn struct {
	addr    string
	conn    net.Conn
	codec   *LTIVCodec
	handler MessageHandler
	mu      sync.Mutex
	buf     []byte
	closed  bool
}

func NewConn(handler MessageHandler) *Conn {
	return &Conn{
		codec:   NewLTIVCodec(),
		handler: handler,
		buf:     make([]byte, 0, 4096),
	}
}

func (c *Conn) Connect(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	c.conn = conn
	c.addr = addr
	c.closed = false
	go c.readLoop()
	return nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Conn) Send(msg proto.Message) error {
	id := IDByMessage(msg)
	if id == "" {
		return fmt.Errorf("unknown message type: %T", msg)
	}
	payload, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}
	return c.SendRaw(&Message{
		Type:    1,
		Path:    id,
		Payload: payload,
	})
}

func (c *Conn) SendFirst(msg proto.Message) error {
	id := IDByMessage(msg)
	if id == "" {
		return fmt.Errorf("unknown message type: %T", msg)
	}
	payload, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}
	return c.SendRaw(&Message{
		Type:    0,
		Path:    id,
		Payload: payload,
	})
}

func (c *Conn) SendRaw(msg *Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	data, err := c.codec.Encode(msg)
	if err != nil {
		return err
	}
	_, err = c.conn.Write(data)
	return err
}

func (c *Conn) RemoteAddr() string {
	if c.conn == nil {
		return ""
	}
	return c.conn.RemoteAddr().String()
}

func (c *Conn) readLoop() {
	recvBuf := make([]byte, 4096)
	for {
		n, err := c.conn.Read(recvBuf)
		if err != nil {
			if !c.closed {
				c.handler(nil) // nil signals disconnection
			}
			return
		}

		c.buf = append(c.buf, recvBuf[:n]...)

		for {
			consumed, msg, err := c.codec.Decode(c.buf)
			if err != nil {
				break
			}
			if msg == nil {
				break
			}
			c.buf = c.buf[consumed:]
			c.handler(msg)
		}

		// Trim processed bytes
		if len(c.buf) == 0 {
			c.buf = make([]byte, 0, 4096)
		}
	}
}

// parseID converts string msg_id to uint16
func parseID(id string) uint16 {
	var result uint16
	for _, ch := range id {
		result = result*10 + uint16(ch-'0')
	}
	return result
}

// encodeID converts uint16 msg_id to string
func encodeID(id uint16) string {
	return idToString(id)
}

// LittleEndian helpers (used internally)
var le = binary.LittleEndian
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/zyr/workspace/hy_client
go test ./pkg/client/ -run TestConn -v
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/client/conn.go pkg/client/conn_test.go
git commit -m "feat: TCP connection with read loop and protobuf send"
```

---

### Task 6: Client High-Level API

**Files:**
- Create: `pkg/client/client.go`
- Create: `pkg/client/client_test.go`

- [ ] **Step 1: Write failing test for Client handshake + login flow**

Create `pkg/client/client_test.go`:

```go
package client

import (
	"net"
	"testing"
	"time"

	pb "hy_client/pb"

	"google.golang.org/protobuf/proto"
)

func TestClientHandshakeLogin(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		codec := NewLTIVCodec()
		buf := make([]byte, 8192)

		// Read handshake request
		n, _ := conn.Read(buf)
		_, msg, _ := codec.Decode(buf[:n])
		if msg.Path != "10001" {
			t.Errorf("expected handshake req, got path %s", msg.Path)
			return
		}

		// Send handshake response
		rsp := &pb.RspHandShake{AccountUid: "test", RoleId: 1001}
		payload, _ := proto.Marshal(rsp)
		data, _ := codec.Encode(&Message{Type: 1, Path: "10002", Payload: payload})
		conn.Write(data)

		// Read login request
		n, _ = conn.Read(buf)
		_, msg, _ = codec.Decode(buf[:n])
		if msg.Path != "10003" {
			t.Errorf("expected login req, got path %s", msg.Path)
			return
		}

		// Send login response
		loginRsp := &pb.RspAccountLogin{FirstLogin: false, RoleId: 1001}
		payload, _ = proto.Marshal(loginRsp)
		data, _ = codec.Encode(&Message{Type: 1, Path: "10004", Payload: payload})
		conn.Write(data)
	}()

	cfg := Config{Addr: listener.Addr().String(), AccountUID: "test"}
	c := NewClient(cfg)

	err = c.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	rsp, err := c.Handshake()
	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	if rsp.RoleId != 1001 {
		t.Errorf("role_id mismatch: got %d, want 1001", rsp.RoleId)
	}

	loginRsp, err := c.Login()
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if loginRsp.RoleId != 1001 {
		t.Errorf("login role_id mismatch: got %d, want 1001", loginRsp.RoleId)
	}
}

func TestClientRequestResponse(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		codec := NewLTIVCodec()
		buf := make([]byte, 8192)

		// Read request
		n, _ := conn.Read(buf)
		_, msg, _ := codec.Decode(buf[:n])

		// Send matching response (ID = req ID + 1 for same-prefix messages)
		reqID := parseID(msg.Path)
		rspID := reqID + 1
		rspPayload := []byte{0x08, 0x01} // field 1 = 1
		data, _ := codec.Encode(&Message{Type: 1, Path: encodeID(rspID), Payload: rspPayload})
		conn.Write(data)
	}()

	cfg := Config{Addr: listener.Addr().String()}
	c := NewClient(cfg)
	err = c.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Send request and wait for response
	err = c.Request(&pb.ReqBagInfo{})
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
}

func TestClientNotification(t *testing.T) {
	RegisterMessages()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	notifyCh := make(chan proto.Message, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		codec := NewLTIVCodec()

		// Send a push notification
		notify := &pb.NotifyBagUpdate{}
		payload, _ := proto.Marshal(notify)
		data, _ := codec.Encode(&Message{Type: 1, Path: "21003", Payload: payload})
		conn.Write(data)
	}()

	cfg := Config{Addr: listener.Addr().String()}
	c := NewClient(cfg)
	c.OnMessage(func(msg proto.Message) {
		notifyCh <- msg
	})

	err = c.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	select {
	case msg := <-notifyCh:
		if _, ok := msg.(*pb.NotifyBagUpdate); !ok {
			t.Errorf("expected NotifyBagUpdate, got %T", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for notification")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/zyr/workspace/hy_client
go test ./pkg/client/ -run TestClient -v
```

Expected: compilation error.

- [ ] **Step 3: Implement Client**

Create `pkg/client/client.go`:

```go
package client

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	pb "hy_client/pb"

	"google.golang.org/protobuf/proto"
)

const requestTimeout = 10 * time.Second

type Config struct {
	Addr       string
	AccountUID string
}

type pendingRequest struct {
rspID  string
ch     chan proto.Message
}

type Client struct {
	cfg     Config
	conn    *Conn
	roleID  int64

	mu       sync.Mutex
	pendings map[string]*pendingRequest // keyed by rspID

	onMessage func(msg proto.Message)
}

func NewClient(cfg Config) *Client {
	return &Client{
		cfg:      cfg,
		pendings: make(map[string]*pendingRequest),
	}
}

func (c *Client) Connect() error {
	c.conn = NewConn(c.handleMessage)
	return c.conn.Connect(c.cfg.Addr)
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) SetAccountUID(uid string) {
	c.cfg.AccountUID = uid
}

func (c *Client) RoleID() int64 {
	return c.roleID
}

func (c *Client) Handshake() (*pb.RspHandShake, error) {
	req := &pb.ReqHandShake{AccountUid: c.cfg.AccountUID}

	rspMsg, err := c.doRequest(req, c.conn.SendFirst)
	if err != nil {
		return nil, fmt.Errorf("handshake: %w", err)
	}

	rsp, ok := rspMsg.(*pb.RspHandShake)
	if !ok {
		return nil, fmt.Errorf("handshake: unexpected response type %T", rspMsg)
	}

	c.roleID = rsp.RoleId
	return rsp, nil
}

func (c *Client) Login() (*pb.RspAccountLogin, error) {
	req := &pb.ReqAccountLogin{
		RoleId:     c.roleID,
		ClientInfo: `{"client":"hy_client","platform":"console"}`,
	}

	rspMsg, err := c.doRequest(req, c.conn.Send)
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	rsp, ok := rspMsg.(*pb.RspAccountLogin)
	if !ok {
		return nil, fmt.Errorf("login: unexpected response type %T", rspMsg)
	}

	return rsp, nil
}

func (c *Client) Send(msg proto.Message) error {
	return c.conn.Send(msg)
}

func (c *Client) Request(msg proto.Message) error {
	_, err := c.doRequest(msg, c.conn.Send)
	return err
}

func (c *Client) RequestWithResponse(msg proto.Message) (proto.Message, error) {
	return c.doRequest(msg, c.conn.Send)
}

func (c *Client) OnMessage(handler func(msg proto.Message)) {
	c.onMessage = handler
}

type sendFunc func(proto.Message) error

func (c *Client) doRequest(req proto.Message, send sendFunc) (proto.Message, error) {
	rspID := responseIDFor(req)

	ch := make(chan proto.Message, 1)
	c.mu.Lock()
	c.pendings[rspID] = &pendingRequest{rspID: rspID, ch: ch}
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pendings, rspID)
		c.mu.Unlock()
	}()

	if err := send(req); err != nil {
		return nil, err
	}

	select {
	case rsp := <-ch:
		return rsp, nil
	case <-time.After(requestTimeout):
		return nil, fmt.Errorf("timeout waiting for response %s", rspID)
	}
}

func (c *Client) handleMessage(msg *Message) {
	if msg == nil {
		// Disconnection
		return
	}

	// Deserialize payload into proto message
	protoMsg := NewMessageByID(msg.Path)
	if protoMsg == nil {
		return
	}

	if err := proto.Unmarshal(msg.Payload, protoMsg); err != nil {
		return
	}

	// Check if this matches a pending request
	c.mu.Lock()
	pending, ok := c.pendings[msg.Path]
	c.mu.Unlock()

	if ok {
		pending.ch <- protoMsg
		return
	}

	// Not a pending response — dispatch as notification/push
	if c.onMessage != nil {
		c.onMessage(protoMsg)
	}
}

// responseIDFor calculates the expected response message ID for a request.
// Convention: Req prefix → Rsp prefix, ID = reqID + 1 (same module prefix, consecutive numbering)
func responseIDFor(req proto.Message) string {
	reqID := IDByMessage(req)
	id, err := strconv.ParseUint(reqID, 10, 32)
	if err != nil {
		return ""
	}
	return strconv.FormatUint(id+1, 10)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/zyr/workspace/hy_client
go test ./pkg/client/ -run TestClient -v
```

Expected: all PASS.

- [ ] **Step 5: Run all tests**

```bash
cd /home/zyr/workspace/hy_client
go test ./pkg/client/ -v
```

Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/client/client.go pkg/client/client_test.go
git commit -m "feat: Client high-level API with handshake, login, request/response"
```

---

### Task 7: REPL Command System

**Files:**
- Create: `cmd/hy/commands.go`

- [ ] **Step 1: Create command registry and all commands**

Create `cmd/hy/commands.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"hy_client/pb"
	"hy_client/pkg/client"

	"google.golang.org/protobuf/proto"
)

type Command struct {
	Name   string
	Help   string
	Params []string
	Exec   func(c *client.Client, args []string) error
}

var commands = map[string]*Command{}

func register(cmd *Command) {
	commands[cmd.Name] = cmd
}

func init() {
	// --- basic ---
	register(&Command{
		Name:   "basic.info",
		Help:   "Get basic role info",
		Params: []string{},
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqBasicInfo{})
		},
	})
	register(&Command{
		Name:   "basic.set_name",
		Help:   "Set role name",
		Params: []string{"name"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: basic.set_name <name>")
			}
			return c.Request(&pb.ReqBasicSetName{Name: args[0]})
		},
	})
	register(&Command{
		Name:   "basic.set_head",
		Help:   "Set role head icon",
		Params: []string{"head"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: basic.set_head <head>")
			}
			return c.Request(&pb.ReqBasicSetHead{Head: args[0]})
		},
	})

	// --- bag ---
	register(&Command{
		Name:   "bag.info",
		Help:   "Get bag info",
		Params: []string{},
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqBagInfo{})
		},
	})

	// --- sign ---
	register(&Command{
		Name:   "sign.info",
		Help:   "Get sign-in info",
		Params: []string{},
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqSignInfo{})
		},
	})
	register(&Command{
		Name:   "sign.draw",
		Help:   "Draw daily sign-in reward",
		Params: []string{},
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqSignDraw{})
		},
	})
	register(&Command{
		Name:   "sign.patch",
		Help:   "Patch missed sign-in days",
		Params: []string{"times"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: sign.patch <times>")
			}
			times, err := strconv.ParseUint(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid times: %v", err)
			}
			return c.Request(&pb.ReqSignPatch{PatchTimes: uint32(times)})
		},
	})
	register(&Command{
		Name:   "sign.accum_draw",
		Help:   "Draw accumulated sign-in reward",
		Params: []string{"stage"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: sign.accum_draw <stage>")
			}
			stage, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid stage: %v", err)
			}
			return c.Request(&pb.ReqSignAccumDraw{Stage: int32(stage)})
		},
	})

	// --- friend ---
	register(&Command{
		Name:   "friend.info",
		Help:   "Get friend list info",
		Params: []string{},
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqFriendInfo{})
		},
	})
	register(&Command{
		Name:   "friend.apply",
		Help:   "Apply to add friends",
		Params: []string{"[role_id,...] or [{\"role_id\":X,\"source\":\"Y\"},...]"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.apply [{\"role_id\":1001,\"source\":\"search\"}]")
			}
			raw := strings.Join(args, " ")
			var applyList []*pb.PFriendApplyData
			if err := json.Unmarshal([]byte(raw), &applyList); err != nil {
				return fmt.Errorf("invalid JSON array: %v", err)
			}
			return c.Request(&pb.ReqFriendApply{ApplyList: applyList})
		},
	})
	register(&Command{
		Name:   "friend.deal_apply",
		Help:   "Accept(1) or reject(0) friend applications",
		Params: []string{"[role_id,...]", "deal(0|1)"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: friend.deal_apply [1001,1002] 1")
			}
			roleIDs, err := parseArrayInt64(args[0])
			if err != nil {
				return fmt.Errorf("invalid role_ids: %v", err)
			}
			deal, err := strconv.ParseInt(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid deal: %v", err)
			}
			return c.Request(&pb.ReqFriendDealApply{RoleId: roleIDs, Deal: int32(deal)})
		},
	})
	register(&Command{
		Name:   "friend.delete",
		Help:   "Delete friends",
		Params: []string{"[role_id,...]"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.delete [1001,1002]")
			}
			roleIDs, err := parseArrayInt64(args[0])
			if err != nil {
				return fmt.Errorf("invalid role_ids: %v", err)
			}
			return c.Request(&pb.ReqFriendDelete{RoleId: roleIDs})
		},
	})
	register(&Command{
		Name:   "friend.send_gift",
		Help:   "Send gift to friend",
		Params: []string{"role_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.send_gift <role_id>")
			}
			roleID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid role_id: %v", err)
			}
			return c.Request(&pb.ReqFriendSendGift{RoleId: roleID})
		},
	})
	register(&Command{
		Name:   "friend.recv_gift",
		Help:   "Receive gift from friend",
		Params: []string{"role_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.recv_gift <role_id>")
			}
			roleID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid role_id: %v", err)
			}
			return c.Request(&pb.ReqFriendRecvGift{RoleId: roleID})
		},
	})

	// --- mahong ---
	register(&Command{
		Name:   "mahong.create_room",
		Help:   "Create mahjong room",
		Params: []string{"game_mode", "play_turn", "max_fan", "max_player"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 4 {
				return fmt.Errorf("usage: mahong.create_room <game_mode> <play_turn> <max_fan> <max_player>")
			}
			mode, _ := strconv.ParseInt(args[0], 10, 32)
			turn, _ := strconv.ParseInt(args[1], 10, 32)
			fan, _ := strconv.ParseInt(args[2], 10, 32)
			player, _ := strconv.ParseInt(args[3], 10, 32)
			return c.Request(&pb.ReqMahongCreateRoom{
				Rule: &pb.PMahongRuleInfo{
					GameMode:  int32(mode),
					PlayTurn:  int32(turn),
					MaxFan:    int32(fan),
					MaxPlayer: int32(player),
				},
			})
		},
	})
	register(&Command{
		Name:   "mahong.join_room",
		Help:   "Join mahjong room",
		Params: []string{"room_id", "identify"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: mahong.join_room <room_id> <identify>")
			}
			roomID, _ := strconv.ParseInt(args[0], 10, 64)
			identify, _ := strconv.ParseInt(args[1], 10, 32)
			return c.Request(&pb.ReqMahongJoinRoom{RoomId: roomID, Identify: int32(identify)})
		},
	})
	register(&Command{
		Name:   "mahong.operate",
		Help:   "Send mahjong operate command",
		Params: []string{"cmd", "val"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: mahong.operate <cmd> <val>")
			}
			cmd, _ := strconv.ParseInt(args[0], 10, 32)
			val, _ := strconv.ParseInt(args[1], 10, 32)
			return c.Request(&pb.ReqMahongOperate{Cmd: int32(cmd), Val: int32(val)})
		},
	})
	register(&Command{
		Name:   "mahong.set_ready",
		Help:   "Set ready state",
		Params: []string{"ready(0|1)"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: mahong.set_ready <ready>")
			}
			ready, _ := strconv.ParseInt(args[0], 10, 32)
			return c.Request(&pb.ReqMahongSetReady{Ready: int32(ready)})
		},
	})
}

// parseArrayInt64 parses "[1001,1002,1003]" into []int64
func parseArrayInt64(s string) ([]int64, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("array must be wrapped in [], got: %s", s)
	}
	s = s[1 : len(s)-1]
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	result := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number: %s", p)
		}
		result = append(result, v)
	}
	return result, nil
}

// printProtoJSON prints a protobuf message as JSON
func printProtoJSON(prefix string, msg proto.Message) {
	name := string(proto.MessageName(msg))
	marshaler := protojson.MarshalOptions{EmitDefaultValues: true}
	jsonBytes, err := marshaler.Marshal(msg)
	if err != nil {
		fmt.Printf("%s %s <marshal error: %v>\n", prefix, name, err)
		return
	}
	fmt.Printf("%s %s %s\n", prefix, name, string(jsonBytes))
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /home/zyr/workspace/hy_client
# We need protojson import
go get google.golang.org/protobuf/encoding/protojson
go build ./cmd/hy/
```

Expected: may fail because main.go doesn't exist yet — that's OK. Just check that commands.go compiles:

```bash
go vet ./cmd/hy/commands.go 2>&1 || true
```

The import of `protojson` will be used later. For now just ensure no syntax errors.

- [ ] **Step 3: Commit**

```bash
git add cmd/hy/commands.go go.mod go.sum
git commit -m "feat: REPL command definitions for all game modules"
```

---

### Task 8: REPL Loop

**Files:**
- Create: `cmd/hy/repl.go`

- [ ] **Step 1: Create REPL implementation**

Create `cmd/hy/repl.go`:

```go
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"hy_client/pkg/client"
)

type REPL struct {
	client *client.Client
	reader *bufio.Reader
}

func NewREPL(c *client.Client) *REPL {
	return &REPL{
		client: c,
		reader: bufio.NewReader(os.Stdin),
	}
}

func (r *REPL) Run() {
	fmt.Println("Type 'help' for available commands, 'quit' to exit.")

	for {
		fmt.Print("hy> ")
		line, err := r.reader.ReadString('\n')
		if err != nil {
			fmt.Printf("\nread error: %v\n", err)
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if line == "quit" || line == "exit" {
			fmt.Println("bye.")
			return
		}

		if line == "help" {
			r.printHelp()
			continue
		}

		if line == "reconnect" {
			r.reconnect()
			continue
		}

		r.execute(line)
	}
}

func (r *REPL) execute(line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}

	name := parts[0]
	args := parts[1:]

	// Rejoin array arguments: if an arg starts with [ and doesn't end with ], merge subsequent args
	args = rejoinArrays(args)

	cmd, ok := commands[name]
	if !ok {
		fmt.Printf("unknown command: %s (type 'help' for commands)\n", name)
		return
	}

	if err := cmd.Exec(r.client, args); err != nil {
		fmt.Printf("error: %v\n", err)
	}
}

// rejoinArrays merges tokens like "[1001," "1002]" into "[1001,1002]"
func rejoinArrays(args []string) []string {
	result := make([]string, 0, len(args))
	var merged strings.Builder
	merging := false

	for _, arg := range args {
		if merging {
			merged.WriteString(arg)
			if strings.HasSuffix(arg, "]") {
				result = append(result, merged.String())
				merged.Reset()
				merging = false
			}
			continue
		}

		if strings.HasPrefix(arg, "[") && !strings.HasSuffix(arg, "]") {
			merging = true
			merged.WriteString(arg)
			continue
		}

		result = append(result, arg)
	}

	if merging {
		result = append(result, merged.String())
	}

	return result
}

func (r *REPL) printHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  help                    Show this help")
	fmt.Println("  quit                    Exit")
	fmt.Println("  reconnect               Reconnect to server")
	fmt.Println("")
	for name, cmd := range commands {
		paramsStr := strings.Join(cmd.Params, " ")
		if paramsStr != "" {
			fmt.Printf("  %-25s %s (%s)\n", name, cmd.Help, paramsStr)
		} else {
			fmt.Printf("  %-25s %s\n", name, cmd.Help)
		}
	}
}

func (r *REPL) reconnect() {
	r.client.Close()
	fmt.Println("disconnected. reconnecting...")
	if err := r.client.Connect(); err != nil {
		fmt.Printf("connect failed: %v\n", err)
		return
	}
	fmt.Println("connected.")

	rsp, err := r.client.Handshake()
	if err != nil {
		fmt.Printf("handshake failed: %v\n", err)
		return
	}
	fmt.Printf("handshake ok, role_id=%d\n", rsp.RoleId)

	_, err = r.client.Login()
	if err != nil {
		fmt.Printf("login failed: %v\n", err)
		return
	}
	fmt.Println("login ok.")
}
```

- [ ] **Step 2: Commit**

```bash
git add cmd/hy/repl.go
git commit -m "feat: REPL loop with command parsing, help, reconnect"
```

---

### Task 9: Main Entry Point

**Files:**
- Create: `cmd/hy/main.go`
- Modify: `go.mod` (add BurntSushi/toml dependency)

- [ ] **Step 1: Add TOML dependency**

```bash
cd /home/zyr/workspace/hy_client
go get github.com/BurntSushi/toml
```

- [ ] **Step 2: Create main.go**

Create `cmd/hy/main.go`:

```go
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"hy_client/pkg/client"

	"github.com/BurntSushi/toml"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type config struct {
	Server struct {
		Addr    string `toml:"addr"`
		Account string `toml:"account"`
	} `toml:"server"`
}

func main() {
	addr := flag.String("addr", "", "server address (host:port)")
	account := flag.String("account", "", "account uid")
	configFile := flag.String("config", "", "config file path")
	flag.Parse()

	// Load defaults from config file
	cfg := &config{}
	if *configFile != "" {
		if _, err := toml.DecodeFile(*configFile, &cfg); err != nil {
			fmt.Printf("failed to load config: %v\n", err)
			os.Exit(1)
		}
	}

	// Command line overrides config file
	serverAddr := cfg.Server.Addr
	if *addr != "" {
		serverAddr = *addr
	}
	defaultAccount := cfg.Server.Account
	if *account != "" {
		defaultAccount = *account
	}

	if serverAddr == "" {
		fmt.Println("error: server address required (--addr or config file)")
		os.Exit(1)
	}

	// Initialize message registry
	client.RegisterMessages()

	// Prompt for account
	accountUID := promptAccount(defaultAccount)

	// Create client
	c := client.NewClient(client.Config{
		Addr:       serverAddr,
		AccountUID: accountUID,
	})

	// Set up response/notification printer
	c.OnMessage(func(msg proto.Message) {
		printProtoJSON("[push]", msg)
	})

	// Connect
	fmt.Printf("connecting to %s...\n", serverAddr)
	if err := c.Connect(); err != nil {
		fmt.Printf("connect failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("connected.")

	// Handshake
	rsp, err := c.Handshake()
	if err != nil {
		fmt.Printf("handshake failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("handshake ok, role_id=%d\n", rsp.RoleId)

	// Login
	loginRsp, err := c.Login()
	if err != nil {
		fmt.Printf("login failed: %v\n", err)
		os.Exit(1)
	}
	if loginRsp.FirstLogin {
		fmt.Println("first login!")
	}
	fmt.Println("login ok.")

	// Enter REPL
	repl := NewREPL(c)
	repl.Run()

	// Cleanup
	c.Close()
}

func promptAccount(defaultAccount string) string {
	if defaultAccount != "" {
		fmt.Printf("Account [%s]: ", defaultAccount)
	} else {
		fmt.Print("Account: ")
	}

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = trimNewline(input)

	if input == "" {
		if defaultAccount == "" {
			fmt.Println("error: account required")
			os.Exit(1)
		}
		return defaultAccount
	}
	return input
}

func trimNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '\r' {
		s = s[:len(s)-1]
	}
	return s
}
```

- [ ] **Step 3: Build and verify**

```bash
cd /home/zyr/workspace/hy_client
go build -o bin/hy ./cmd/hy/
```

Expected: binary created at `bin/hy`.

- [ ] **Step 4: Run all tests**

```bash
cd /home/zyr/workspace/hy_client
go test ./...
```

Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/hy/main.go go.mod go.sum
git commit -m "feat: main entry point with config, account prompt, login flow"
```

- [ ] **Step 6: Final commit — verify everything builds**

```bash
cd /home/zyr/workspace/hy_client
make build
make test
```

Expected: build succeeds, all tests pass.
