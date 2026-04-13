# Coding Conventions

**Analysis Date:** 2026-04-13

## Naming Patterns

**Files:**
- Lowercase with underscores for directories: `internal/logic`, `internal/event`, `core/gxyactor`
- Descriptive file names: `role_basic.go`, `session_mgr.go`, `gxynet_test.go`
- `_test.go` suffix for test files
- Use English for all filenames

**Functions:**
- Exported functions use PascalCase: `ReqBasicSetName`, `OnModInit`, `OnModStart`
- Private functions use PascalCase for first letter: `isNameValid`, `initRoleModules`
- Handler methods prefixed with Req for incoming requests: `ReqBasicSetName`, `ReqBasicInfo`
- Boolean methods using descriptive names like `isNameValid`

**Variables:**
- Use descriptive names: `RoleBasicState`, `LoginTm`, `CreateTm`
- Avoid short abbreviations except for common Go idioms
- Context parameter named `ctx`

**Types:**
- Structs use PascalCase: `RoleBasicState`, `IRoleModule`
- Interfaces prefixed with I: `IPersistState`, `IRoleModule`
- Embedding pattern for shared functionality: `RoleBasicState`, `RoleModule`

## Code Style

**Formatting:**
- Uses GoFrame CLI with `gf build -ew` for enforced formatting
- Standard Go formatting rules applied
- Import organization using GoFrame patterns

**Linting:**
- No explicit `.golangci.yml` found, but relies on GoFrame CLI for code quality
- Error messages are concise but descriptive

**Documentation:**
- Structs with bson tags for MongoDB serialization
- Comments in Chinese for business logic explanation: // 创角时间
- Few TSDoc/JSDoc comments observed

## Import Organization

**Order:**
```go
import (
    "context"
    "fmt"
    "gserver/protocol/pb"
    "time"
    "github.com/gogf/gf/v2/frame/g"
)
```

**Path Aliases:**
- Local packages use relative imports: `"gserver/protocol/pb"`
- External packages are full URLs
- No path aliases configured

## Error Handling

**Patterns:**
```go
// Return error immediately
func (r *RoleBasic) ReqBasicSetName(ctx context.Context, req *pb.ReqBasicSetName) (*pb.RspBasicSetName, error) {
    if !r.isNameValid(req.Name) {
        return nil, fmt.Errorf("name unvalid:%s", req.Name)
    }
    // ... rest of implementation
}
```

- Use `fmt.Errorf` for custom error messages
- Return `nil` as first parameter when error occurs
- Error messages are brief but descriptive

## Logging

**Framework:** GoFrame glog

**Patterns:**
```go
g.Log().Debugf(ctx, `receive say: %+v`, req)
t.Logf("role basic : %v", r.Basic)
```

- Use structured logging with GoFrame
- Test files use `t.Logf` for debug information
- Context-aware logging with module and roleID keys
- No logging levels enforced in code

## Comments

**When to Comment:**
- Business logic in Chinese for clarity
- Database field comments in Chinese: // 创角时间
- TODO comments for unimplemented tests

**JSDoc/TSDoc:**
- Not consistently used throughout codebase

## Function Design

**Size:**
- Functions are kept small and focused
- Handler methods typically 10-20 lines
- Business logic separated into dedicated methods

**Parameters:**
- Consistent use of context.Context as first parameter
- Request/response types generated from protobuf
- Error as last return parameter

**Return Values:**
- Error always as last return parameter
- Return pointer types for struct responses
- Consistent null/nil checks

## Module Design

**Exports:**
- Public methods prefixed with App or grain
- Core functionality exposed through interfaces
- Grain pattern for actor-based modules

**Barrel Files:**
- Not commonly used
- Imports are specific to their usage

## Patterns

**Actor Pattern:**
```go
type RoleBasic struct {
    RoleModule
    RoleBasicState
}

var _ IRoleModule = (*RoleBasic)(nil)
```

- Interface assertions for type safety
- Composition over inheritance
- Module-based architecture

**Persistence:**
```go
func (r *RoleBasic) PersistState() IPersistState {
    return &r.RoleBasicState
}
```

- Interface-based persistence pattern
- Inline bson tags for MongoDB

---

*Convention analysis: 2026-04-13*