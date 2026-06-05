# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# hy_client

Console debugging client and benchmark tool for "My Garden World" (gserver).

## Build & Run

```bash
git submodule update --init     # clone proto/client submodule
make build                      # build both binaries (cmd/hy + cmd/bench)
make proto                      # regenerate protobuf from proto/client/*.proto
make test                       # go test ./...
go test ./pkg/client/ -v        # verbose SDK tests
go test ./pkg/client/ -run TestCodec        # run single test
go test ./pkg/client/ -run TestCodec -v -count=1  # single test, no cache
```

## Commands

- `./bin/hy` — interactive REPL for manual server debugging
  - `--account-server=http://account.example.com` — account server URL (required for prelogin)
  - `--platform-uid=u_123456` — platform user ID
  - `--platform=guest` — platform identifier (guest/wechat/apple, default: guest)
  - `--client-version=1.0.0` — client version
  - `--addr=127.0.0.1:9000` — override gate address from prelogin
  - `--config=path.toml` — TOML config file
  - All `Req*` protos auto-register as `domain.action` commands (e.g. `bag.info`, `friend.search_player`)
- `./bin/bench -config cmd/bench/bench.yaml` — benchmark bot framework (also supports `account_server` config field for prelogin)

## Architecture

```
cmd/hy/            REPL client — main, commands.go, repl.go, autocmd.go
cmd/bench/         Benchmark framework — bot, manager, actions, state, script runner
pkg/client/        Core SDK shared by both binaries
  client.go        Client struct: connect/handshake/login, doRequest with pending-request tracking
  conn.go          TCP connection with readLoop goroutine, LTIV encode/decode
  codec.go         LTIV transport: [Size:2B LE][Type:1B][ID:2B LE][Payload:protobuf]
  registry.go      Message registry — scans protobuf files for `option (msg_id)`, bidir ID<->type maps
  fieldparse.go    Generic protobuf field parsing from CLI string args (int, string, bool, enum)
pb/                Generated protobuf Go code
proto/client/      Proto source files (gserver submodule)
```

## Key Architectural Patterns

**Auto-command registration** (`cmd/hy/autocmd.go`): At startup, scans all `Req*` protos via the registry and generates CLI commands for them. The domain name is derived from the PascalCase proto name (e.g. `ReqFriendSearchPlayer` → `friend.search_player`). Field types are auto-parsed from the proto schema. Hand-written commands in `commands.go` take priority over auto-registered ones.

**Message registry** (`pkg/client/registry.go`): Uses protobuf reflection to iterate `galaxy.protocol` package messages. Messages with `option (msg_id)` are indexed by numeric ID and Go type — enabling bidirectional lookup (ID↔type↔name). This drives both the codec (wire format uses numeric IDs) and auto-command generation.

**Protocol**: LTIV framing — 2-byte little-endian size, 1-byte type (0=handshake, 1=data), 2-byte little-endian message ID, variable-length protobuf payload. Max packet size 3MB.

**Request/response matching** (`client.go`): Pending requests tracked by `responseID = reqID + 1`. The response handler also handles `Ack` messages: if an Ack has a non-zero code and matches a pending request's ID, it's routed as a response error rather than pushed as a server message.

**Bench script runner** (`cmd/bench/`): Bot behavior defined entirely in YAML as a script of composable actions (login, plant, water, harvest, wait, loop, etc.). Actions are simple Go functions that send requests and update a `BotState` struct tracking breed/harvest timers.

## Protocol Conventions

- If the wire protocol is missing a field, don't modify the client — tell the user to fix it server-side
- Response ID = request ID + 1 (numeric convention in proto msg_id options)
- Type=0 packets are for handshake only; Type=1 for all other messages

## Login Flow

```
POST /account/prelogin (HTTP)  →  gate.host, gate.port, gate_token, role_id
TCP connect to gate:port
Send ReqHandShake{gate_token}  (as first packet, Type=0)
Receive RspHandShake{account_uid, role_id}
Send ReqAccountLogin{role_id, client_info}
```

- `pkg/client/account.go`: `AccountServerPrelogin()` — HTTP POST to account server, returns gate info + token
- `pkg/client/client.go`: `Handshake(gateToken)` — sends gate token to gate server
- `cmd/hy/` flow: prelogin → connect to gate → handshake → login → REPL
- On disconnect: must redo prelogin for a fresh gate_token (see `repl.go:reconnect()`)
- Proto submodule URL: `https://github.com/zylikedream/gserver_protocol.git`

## Testing

Tests focus on the SDK layer (`pkg/client/`) and bench actions (`cmd/bench/`). The client code is tested with a mock TCP server in `client_test.go`. Codec tests verify LTIV encode/decode round-trips with various payload sizes. Registry tests validate proto message scanning and ID lookup. Bench action tests simulate server responses to verify state transitions.
