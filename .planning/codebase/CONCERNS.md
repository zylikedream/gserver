# Codebase Concerns

**Analysis Date:** 2026-04-13

## Tech Debt

**Session Management Race Conditions:**
- Issue: Global session manager (`sessMgr`) in `/home/zyr/workspace/gserver/apps/gateway/internal/logic/session_mgr.go` lacks proper synchronization
- Files: `[apps/gateway/internal/logic/session_mgr.go]`
- Impact: Concurrent access to session map could cause data corruption or race conditions
- Fix approach: Implement proper mutex protection for session manager operations

**MongoDB Error Handling:**
- Issue: MongoDB operations in `/home/zyr/workspace/gserver/core/gxymongo/mongo.go` return generic errors without context
- Files: `[core/gxymongo/mongo.go:111-147]`
- Impact: Hard to trace database errors in production, missing retry mechanisms
- Fix approach: Implement retry logic with exponential backoff and detailed error logging

**Global State Management:**
- Issue: Global `apps` map in `/home/zyr/workspace/gserver/core/gxyapp.go/app.go` uses unexported map without protection
- Files: `[core/gxyapp.go/app.go:7]`
- Impact: Concurrent access could cause race conditions during registration
- Fix approach: Use sync.Map or add proper locking mechanism

## Known Bugs

**Memory Leaks in Grain Manager:**
- Symptoms: Actor references may not be properly cleaned up during deregistration
- Files: `[core/gxyactor/grain_manager.go:279-285]`
- Trigger: Frequent grain creation/destruction cycles
- Workaround: Manual cleanup required

**Missing Session State Validation:**
- Symptoms: Sessions can transition between states without proper validation
- Files: `[apps/gateway/internal/logic/session.go:34-41]`
- Trigger: Rapid connection/disconnection cycles
- Workaround: Add state machine validation

## Security Considerations

**Session Hijacking Risk:**
- Risk: Session IDs aren't validated properly during role binding
- Files: `[apps/gateway/internal/logic/session.go:44-51]`
- Current mitigation: Basic PID validation
- Recommendations: Implement session token authentication and role binding verification

**Input Validation:**
- Risk: Client messages processed without proper validation
- Files: `[apps/role/internal/logic/role_main.go:15-25]`
- Current mitigation: Basic canHandleMsg check
- Recommendations: Add comprehensive input validation and sanitization

## Performance Bottlenecks

**Grain Location Lookups:**
- Problem: Every grain access requires registry lookup
- Files: `[core/gxyactor/grain_manager.go:303-328]`
- Cause: No local caching of grain locations
- Improvement path: Implement grain location cache with TTL

**MongoDB Connection Management:**
- Problem: Each operation creates new connections
- Files: `[core/gxymongo/mongo.go:111-147]`
- Cause: Missing connection pooling configuration
- Improvement path: Implement connection pool with proper sizing

## Fragile Areas

**Error Propagation Chain:**
- Files: `[core/gxyactor/actor.go:71-82]`, `[core/gxyactor/actor.go:84-100]`
- Why fragile: Multiple layers of error wrapping make debugging difficult
- Safe modification: Implement unified error type with context preservation
- Test coverage: Limited error path testing

**Message Handling:**
- Files: `[apps/gateway/internal/logic/session.go:71-90]`
- Why fragile: Type assertions without error handling could panic
- Safe modification: Add proper type validation and recovery
- Test coverage: Missing unit tests for message handling edge cases

## Scaling Limits

**Session Memory Usage:**
- Current capacity: Limited by map implementation
- Limit: No session cleanup mechanism for disconnected clients
- Scaling path: Implement session expiration and cleanup routines

**Actor System:**
- Current capacity: 5 grains per activator
- Limit: Fixed pool size doesn't adapt to load
- Scaling path: Dynamic pool sizing with load-based adjustment

## Dependencies at Risk

**Protoactor-go:**
- Risk: Development version (v0.0.0-20250909165758-e952b3c0850e)
- Impact: Potential breaking changes or instability
- Migration plan: Monitor for stable releases and plan upgrade

**Gogf Framework:**
- Risk: Heavy dependency on specific framework features
- Impact: Potential vendor lock-in
- Migration plan: Extract core abstractions from framework dependencies

## Missing Critical Features

**Circuit Breaker:**
- Problem: No protection against cascading failures
- Blocks: Resilient distributed system operation

**Rate Limiting:**
- Problem: No protection against message flooding
- Blocks: System stability under heavy load

## Test Coverage Gaps

**Actor Behavior:**
- What's not tested: Actor message handling and state transitions
- Files: `[apps/role/internal/logic/role_main.go]`, `[apps/gateway/internal/logic/session.go]`
- Risk: Actor system bugs go undetected
- Priority: High

**Database Operations:**
- What's not tested: MongoDB error scenarios and retry logic
- Files: `[core/gxymongo/mongo.go]`
- Risk: Database failures cause service disruption
- Priority: High

---

*Concerns audit: 2026-04-13*