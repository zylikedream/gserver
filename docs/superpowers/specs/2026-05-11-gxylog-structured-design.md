# gxylog Structured Logging Design

## Goal

Add structured field support to gxylog on top of GoFrame glog, with zap-like API.

## API

```go
gxylog.Info(ctx, "guild set position", gxylog.Str("member", roleID), gxylog.Num("pos", position))
```

Output: `2026-05-11 12:00:00 [INFO] guild set position, member=123, pos=2`

## Field Type

```go
type Field struct {
    Key   string
    Value string
}
```

## Constructors

| Function | Signature | Notes |
|----------|-----------|-------|
| Str | `(key, val string) Field` | String values |
| Num | `[T Number](key string, val T) Field` | All integer types via `%d` |
| Bool | `(key string, val bool) Field` | Boolean values |
| Err | `(err error) Field` | key fixed to "error" |
| Any | `(key string, val any) Field` | Fallback via `%v` |

Number constraint covers: `~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64`

## Level Functions

```go
func Info(ctx, msg, ...Field)   // glog.Info
func Debug(ctx, msg, ...Field)  // glog.Debug
func Warn(ctx, msg, ...Field)   // glog.Warning
func Error(ctx, msg, ...Field)  // glog.Error
func Fatal(ctx, msg, ...Field)  // glog.Fatal
```

Each formats fields via `formatFields(msg, fields)` then passes to glog.

## formatFields

- No fields: return msg as-is
- With fields: `msg, key1=val1, key2=val2` (comma-space separated)

## Decisions

- **Underlying engine**: GoFrame glog (unchanged)
- **Migration**: Coexist — new code uses gxylog, old glog calls stay (129 calls)
- **Output format**: key=value text (not JSON)
- **Approach**: Field formatting + glog passthrough (no context/hooks)
- **File**: All changes in `core/gxylog/log.go`, existing API preserved
