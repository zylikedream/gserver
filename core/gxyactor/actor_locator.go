package gxyactor

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"gserver/core/gxylog"

	"github.com/cockroachdb/errors"
	"github.com/redis/go-redis/v9"
)

const (
	actorLocateLeaseTTL       = 15 * time.Second
	actorLocateLeaseHeartbeat = 5 * time.Second
	actorLocateOwnerSeparator = "|"
)

var (
	errActorLocatorLeaseInvalid  = errors.New("actor locator lease is invalid")
	errActorLocatorLeaseDeadline = errors.New("actor locator lease deadline exceeded")
)

// ActorOwner identifies the node and ownership generation for one actor.
type ActorOwner struct {
	NodeID string
	Epoch  uint64
}

type actorLocator struct {
	redis             redis.UniversalClient
	nodeID            string
	leaseToken        string
	leaseTTL          time.Duration
	heartbeatInterval time.Duration
	renewLease        func(context.Context) (bool, error)
	leaseDeadline     atomic.Int64
	fenced            atomic.Bool
}

type claimResult string

const (
	claimAcquired   claimResult = "acquired"
	claimAlreadyOwn claimResult = "already_owned"
	claimOwnedOther claimResult = "owned_by_other"
)

const actorLocatorClaimScript = `
local ownerKey = KEYS[1]
local candidateLeaseKey = KEYS[2]
local epochKey = KEYS[3]
local candidateNode = ARGV[1]
local candidateToken = ARGV[2]
if redis.call("GET", candidateLeaseKey) ~= candidateToken then
    return {"invalid_lease", ""}
end

local current = redis.call("GET", ownerKey)
if current then
    local currentNode, _, currentToken = string.match(current, "^([^|]+)|([0-9]+)|(.+)$")
    if currentNode then
        local currentLease = redis.call("GET", "gserver:locate:node:lease:" .. currentNode)
        if currentLease == currentToken then
            if currentNode == candidateNode then
                return {"already_owned", current}
            end
            return {"owned_by_other", current}
        end
    end
end

local epoch = redis.call("INCR", epochKey)
local owner = candidateNode .. "|" .. epoch .. "|" .. candidateToken
redis.call("SET", ownerKey, owner)
return {"acquired", owner}
`

const actorLocatorReleaseScript = `
local ownerKey = KEYS[1]
local expected = ARGV[1]
if redis.call("GET", ownerKey) == expected then
    redis.call("DEL", ownerKey)
    return 1
end
return 0
`

const actorLocatorReleaseLeaseScript = `
local leaseKey = KEYS[1]
if redis.call("GET", leaseKey) == ARGV[1] then
    redis.call("DEL", leaseKey)
    return 1
end
return 0
`

const actorLocatorRenewLeaseScript = `
local leaseKey = KEYS[1]
if redis.call("GET", leaseKey) == ARGV[1] then
    return redis.call("PEXPIRE", leaseKey, ARGV[2])
end
return 0
`
const actorLocatorLocateScript = `
local owner = redis.call("GET", KEYS[1])
if not owner then
    return ""
end
local node, _, token = string.match(owner, "^([^|]+)|([0-9]+)|(.+)$")
if not node then
    return ""
end
if redis.call("GET", "gserver:locate:node:lease:" .. node) ~= token then
    return ""
end
return owner
`

func newActorLocator(client redis.UniversalClient, nodeID, leaseToken string) *actorLocator {
	return &actorLocator{
		redis:             client,
		nodeID:            nodeID,
		leaseToken:        leaseToken,
		leaseTTL:          actorLocateLeaseTTL,
		heartbeatInterval: actorLocateLeaseHeartbeat,
	}
}

func actorLocatorOwnerKey(kind, id string) string {
	return getActorLocateKey(kind, id)
}

func actorLocatorLeaseKey(nodeID string) string {
	return fmt.Sprintf("%s:lease:%s", redisLocatePrefix, nodeID)
}

func actorLocatorEpochKey() string {
	return redisLocatePrefix + ":epoch"
}

func (l *actorLocator) ensureClient() error {
	if l.redis == nil {
		return errors.New("actor locator Redis client is not initialized")
	}
	return nil
}

func (l *actorLocator) acquireNodeLease(ctx context.Context) error {
	if err := l.ensureClient(); err != nil {
		return err
	}
	started := time.Now()
	ok, err := l.redis.SetNX(ctx, actorLocatorLeaseKey(l.nodeID), l.leaseToken, l.leaseTTL).Result()
	if err != nil {
		return errors.Wrap(err, "acquire actor node lease")
	}
	if !ok {
		return errors.Newf("actor node lease already held: %s", l.nodeID)
	}
	l.confirmLease(started)
	return nil
}

func (l *actorLocator) renewNodeLease(ctx context.Context) (bool, error) {
	if err := l.ensureClient(); err != nil {
		return false, err
	}
	if l.fenced.Load() {
		return false, errActorLocatorLeaseInvalid
	}
	result, err := l.redis.Eval(ctx, actorLocatorRenewLeaseScript, []string{actorLocatorLeaseKey(l.nodeID)}, l.leaseToken, l.leaseTTL.Milliseconds()).Int64()
	if err != nil {
		return false, errors.Wrap(err, "renew actor node lease")
	}
	return result == 1, nil
}

func (l *actorLocator) claim(ctx context.Context, kind, id string) (ActorOwner, bool, error) {
	if err := l.ensureClient(); err != nil {
		return ActorOwner{}, false, err
	}
	if !l.leaseValid(time.Now()) {
		return ActorOwner{}, false, errActorLocatorLeaseInvalid
	}
	result, err := l.redis.Eval(ctx, actorLocatorClaimScript, []string{
		actorLocatorOwnerKey(kind, id),
		actorLocatorLeaseKey(l.nodeID),
		actorLocatorEpochKey(),
	}, l.nodeID, l.leaseToken).Result()
	if err != nil {
		return ActorOwner{}, false, errors.Wrap(err, "claim actor owner")
	}
	if !l.leaseValid(time.Now()) {
		return ActorOwner{}, false, errActorLocatorLeaseInvalid
	}
	parts, ok := result.([]interface{})
	if !ok || len(parts) != 2 {
		return ActorOwner{}, false, errors.Newf("unexpected actor claim result: %T", result)
	}
	status, ok := redisString(parts[0])
	if !ok {
		return ActorOwner{}, false, errors.New("actor claim result status is not a string")
	}
	ownerValue, ok := redisString(parts[1])
	if !ok {
		return ActorOwner{}, false, errors.New("actor claim result owner is not a string")
	}
	owner, err := decodeActorOwner(ownerValue)
	if err != nil {
		return ActorOwner{}, false, err
	}
	switch claimResult(status) {
	case claimAcquired:
		return owner, true, nil
	case claimAlreadyOwn:
		return owner, false, nil
	case claimOwnedOther:
		return owner, false, nil
	case "invalid_lease":
		return ActorOwner{}, false, errActorLocatorLeaseInvalid
	default:
		return ActorOwner{}, false, errors.Newf("unknown actor claim result: %s", status)
	}
}
func (l *actorLocator) releaseNodeLease(ctx context.Context) error {
	if err := l.ensureClient(); err != nil {
		return err
	}
	_, err := l.redis.Eval(ctx, actorLocatorReleaseLeaseScript, []string{actorLocatorLeaseKey(l.nodeID)}, l.leaseToken).Result()
	if err != nil {
		return errors.Wrap(err, "release actor node lease")
	}
	return nil
}

func (l *actorLocator) locate(ctx context.Context, kind, id string) (ActorOwner, error) {
	if err := l.ensureClient(); err != nil {
		return ActorOwner{}, err
	}
	value, err := l.redis.Eval(ctx, actorLocatorLocateScript, []string{actorLocatorOwnerKey(kind, id)}).Result()
	if err != nil {
		return ActorOwner{}, errors.Wrap(err, "locate actor owner")
	}
	ownerValue, ok := redisString(value)
	if !ok {
		return ActorOwner{}, errors.Newf("unexpected actor locate result: %T", value)
	}
	if ownerValue == "" {
		return ActorOwner{}, nil
	}
	return decodeActorOwner(ownerValue)
}

func (l *actorLocator) release(ctx context.Context, kind, id string, owner ActorOwner) (bool, error) {
	if err := l.ensureClient(); err != nil {
		return false, err
	}
	result, err := l.redis.Eval(ctx, actorLocatorReleaseScript, []string{actorLocatorOwnerKey(kind, id)}, encodeActorOwner(owner, l.leaseToken)).Int64()
	if err != nil {
		return false, errors.Wrap(err, "release actor owner")
	}
	return result == 1, nil
}

func (l *actorLocator) confirmLease(started time.Time) {
	if l.fenced.Load() {
		return
	}
	l.leaseDeadline.Store(started.Add(l.leaseTTL).UnixNano())
}

func (l *actorLocator) leaseValid(now time.Time) bool {
	deadline := l.leaseDeadline.Load()
	return !l.fenced.Load() && deadline > 0 && now.UnixNano() < deadline
}

type leaseRenewResult struct {
	refreshed bool
	err       error
	started   time.Time
	finished  time.Time
}

func (l *actorLocator) startLeaseHeartbeat(ctx context.Context, onLost func(error)) func() {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(l.heartbeatInterval)
		defer ticker.Stop()
		deadlineTimer := time.NewTimer(time.Until(time.Unix(0, l.leaseDeadline.Load())))
		defer deadlineTimer.Stop()
		renew := l.renewNodeLease
		if l.renewLease != nil {
			renew = l.renewLease
		}
		renewResults := make(chan leaseRenewResult, 1)
		renewing := false
		var renewCancel context.CancelFunc
		var lastErr error
		fence := func(err error) {
			l.fenced.Store(true)
			if renewCancel != nil {
				renewCancel()
			}
			onLost(err)
		}
		for {
			select {
			case <-heartbeatCtx.Done():
				if renewCancel != nil {
					renewCancel()
				}
				return
			case <-deadlineTimer.C:
				if lastErr != nil {
					fence(errors.Wrap(lastErr, errActorLocatorLeaseDeadline.Error()))
				} else {
					fence(errActorLocatorLeaseDeadline)
				}
				return
			case <-ticker.C:
				if renewing {
					continue
				}
				renewing = true
				started := time.Now()
				renewCtx, cancelRenew := context.WithCancel(heartbeatCtx)
				renewCancel = cancelRenew
				go func() {
					refreshed, err := renew(renewCtx)
					result := leaseRenewResult{refreshed: refreshed, err: err, started: started, finished: time.Now()}
					select {
					case renewResults <- result:
					case <-heartbeatCtx.Done():
					}
				}()
			case result := <-renewResults:
				renewing = false
				renewCancel()
				renewCancel = nil
				if result.finished.UnixNano() >= l.leaseDeadline.Load() {
					fence(errActorLocatorLeaseDeadline)
					return
				}
				if result.err != nil {
					lastErr = result.err
					gxylog.Warn(ctx, "renew actor node lease failed", gxylog.Str("node", l.nodeID), gxylog.Err(result.err))
					continue
				}
				if !result.refreshed {
					fence(errActorLocatorLeaseInvalid)
					return
				}
				l.confirmLease(result.started)
				lastErr = nil
				if !deadlineTimer.Stop() {
					select {
					case <-deadlineTimer.C:
					default:
					}
				}
				deadlineTimer.Reset(time.Until(time.Unix(0, l.leaseDeadline.Load())))
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func encodeActorOwner(owner ActorOwner, leaseToken string) string {
	return strings.Join([]string{owner.NodeID, strconv.FormatUint(owner.Epoch, 10), leaseToken}, actorLocateOwnerSeparator)
}

func decodeActorOwner(value string) (ActorOwner, error) {
	parts := strings.SplitN(value, actorLocateOwnerSeparator, 3)
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
		return ActorOwner{}, errors.Newf("invalid actor owner value: %q", value)
	}
	epoch, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return ActorOwner{}, errors.Wrapf(err, "parse actor owner epoch %q", value)
	}
	if epoch == 0 {
		return ActorOwner{}, errors.Newf("invalid actor owner epoch: %q", value)
	}
	return ActorOwner{NodeID: parts[0], Epoch: epoch}, nil
}

func redisString(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case []byte:
		return string(value), true
	default:
		return "", false
	}
}
