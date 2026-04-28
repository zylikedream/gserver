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
