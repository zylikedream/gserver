# Repository Guidelines

## Project Structure & Module Organization

This Go 1.25 game server uses an actor/module architecture. Shared framework packages live under `core/` for actor, networking, Redis, PostgreSQL/GORM, discovery, timers, MQ, and logging. Runtime entrypoint code is in `node/`.

Business apps live in `src/apps/`: `gateway` handles TCP sessions, while `role` owns player actors and modules such as Basic, Bag, Flower, Plot, and GM. Shared helpers are in `src/lib/` and `src/util/`. Generated protobuf Go files are in `protocol/pb/`; source proto files are under `protocol/client/` and `protocol/server/`. Game config code lives in `gameconfig/`; docs live in `docs/`.

## Build, Test, and Development Commands

- `go build ./...`: compile all packages.
- `make build`: build using GoFrame CLI settings from `hack/`.
- `go test ./...`: run the full Go test suite.
- `go test ./src/apps/role/internal/logic`: run role-module tests.
- `go run node/main.go --config config/game.toml`: start a local node.
- `make pb`: regenerate protobuf files and remove `omitempty` tags.
- `make pbids`: show protobuf message ID ranges.
- `make newproto MOD=name`: create a client proto with the next ID range.

## Coding Style & Naming Conventions

Use `gofmt` on changed Go files. There is no configured linter in the Makefile. Prefer package-local conventions. Actor types and role modules use exported CamelCase names such as `RoleMain`, `RoleBag`, and `NewRoleMain`; files use snake_case, for example `role_main.go`.

For persistence structs, keep explicit `gorm:"column:name"` tags. For protocol changes, update `.proto` first, then regenerate `protocol/pb`.

## Testing Guidelines

Tests use Go’s standard `testing` package, with `gomonkey` in role-module tests for method patching. Put tests beside the package under test and name files `*_test.go`. Prefer targeted package tests while iterating, then run `go test ./...` before handoff. Add focused tests for role behavior, persistence, protobuf handlers, and resource or timer edge cases.

## Commit & Pull Request Guidelines

Recent history uses short conventional prefixes such as `feat:`, `fix:`, `test:`, `docs:`, and scoped forms like `feat(role):`. Keep subjects concise and action-oriented; Chinese descriptions are acceptable.

Pull requests should explain the behavioral change, list affected modules, mention generated files, and include test results. For protocol changes, include the proto file, regenerated `protocol/pb` output, and the `make pbids` range.

## Security & Configuration Tips

Do not commit local secrets or machine-specific endpoints. Runtime configuration belongs in `config/*.toml`; verify PostgreSQL, Redis, and Consul before running locally. Treat `protocol/` and `gameconfig/` as generated or synchronized inputs unless the task explicitly changes them.
