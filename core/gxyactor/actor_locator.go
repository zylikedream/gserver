package gxyactor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	redisLocatePrefix         = "gserver:locate:node"
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

// ZeroActorOwner 是无归属的零值。Locate/Claim 未取得归属时返回它。
var ZeroActorOwner ActorOwner

// IsZero 报告该归属是否为空(无归属):节点与世代都为空。
// Locate/Claim 未取得归属时返回零值,据此区分"有/无 owner"。
func (o ActorOwner) IsZero() bool {
	return o.NodeID == "" && o.Epoch == 0
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
	claimAcquired     claimResult = "acquired"
	claimAlreadyOwn   claimResult = "already_owned"
	claimOwnedOther   claimResult = "owned_by_other"
	claimInvalidLease claimResult = "invalid_lease"
)

// actorLocatorClaimScript 在一次 Redis Lua 调用内完成候选节点校验和 owner 仲裁。
// 返回值为 {状态, owner值}; 状态由 claimResult 及 invalid_lease 分支解释。
const actorLocatorClaimScript = `
-- KEYS: 玩家 owner、候选节点 lease、全局 epoch；ARGV: 候选节点和 lease token。
local ownerKey = KEYS[1]
local candidateLeaseKey = KEYS[2]
local epochKey = KEYS[3]
local candidateNode = ARGV[1]
local candidateToken = ARGV[2]

-- 候选节点无法证明自己仍持有 lease 时必须 fail closed，禁止抢占。
if redis.call("GET", candidateLeaseKey) ~= candidateToken then
    return {"invalid_lease", ""}
end

local current = redis.call("GET", ownerKey)
if current then
    local currentNode, _, currentToken = string.match(current, "^([^|]+)|([0-9]+)|(.+)$")
    if currentNode then
        local currentLease = redis.call("GET", "gserver:locate:node:lease:" .. currentNode)
        -- 只有 owner 节点 lease 仍与 owner 中的 token 匹配，才算活跃 owner。
        if currentLease == currentToken then
            if currentNode == candidateNode then
                -- 同一节点重复 Claim：保留原 epoch，避免无意义地生成新 owner。
                return {"already_owned", current}
            end
            -- 其他节点仍是活跃 owner：候选节点不能越权接管。
            return {"owned_by_other", current}
        end
    end
end

-- owner 缺失、格式非法或原 owner lease 已失效：递增 epoch 后原子接管。
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

// newActorLocator 创建所有权定位器。
//
// nodeID 是路由身份(稳定节点名);leaseToken 每实例随机生成,因此同一节点名
// 的新旧实例在所有权记录里可被区分——这是接管时能正确递增世代的依据(ADR 0010)。
func newActorLocator(client redis.UniversalClient, nodeID string) *actorLocator {
	return &actorLocator{
		redis:             client,
		nodeID:            nodeID,
		leaseToken:        newLeaseToken(),
		leaseTTL:          actorLocateLeaseTTL,
		heartbeatInterval: actorLocateLeaseHeartbeat,
	}
}

// newLeaseToken 生成每实例唯一的租约令牌。
func newLeaseToken() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// 随机源不可用时退回时间戳,仍保证实例间不同。
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf[:])
}

func actorLocatorLeaseKey(nodeID string) string {
	return fmt.Sprintf("%s:lease:%s", redisLocatePrefix, nodeID)
}

func actorLocatorEpochKey() string {
	return redisLocatePrefix + ":epoch"
}

// nodeLease 是本节点租约的能力面:取得、释放、心跳。
//
// 它是与"每个 actor 的归属记录"不同的关注点,两者粒度与生命周期都不同:
//
//   - 本接口:每节点一份,随 actor 模块启动/停止而生灭;
//   - OwnershipStore:每个 actor 一份,随实例的初始化段/终止路径而生灭。
//
// 方法未导出:租约只有本包这一种实现(基于 Redis 的节点级租约),测试不替换它,
// 因此不需要包外实现。把字段声明成本接口,actor 归属的那些方法就不会和租约
// 混在同一个字段上。
type nodeLease interface {
	acquireNodeLease(ctx context.Context) error
	releaseNodeLease(ctx context.Context) error
	startLeaseHeartbeat(ctx context.Context, onLost func(error)) func()
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

// Claim 取得归属,满足 OwnershipStore。
// 对外用 kind 与 id 是刻意的:存储 API 的键词汇就是标量,调用方手里也是它们;
// 身份在进入存储时构造一次,Redis 键由它派生。
//
// 返回的所有权只在成功时有意义。未取得时返回 ErrNotOwner,不再返回"当前持有者":
// 按现行分工所有权由 actor 自管(ADR 0012),激活层不消费那个值,留着只会让人
// 以为调用方该去清理记录。
func (l *actorLocator) Claim(ctx context.Context, kind string, id string) (ActorOwner, error) {
	k := actorKey{kind: kind, id: id}
	if err := l.ensureClient(); err != nil {
		return ZeroActorOwner, err
	}
	if !l.leaseValid(time.Now()) {
		return ZeroActorOwner, errActorLocatorLeaseInvalid
	}
	result, err := l.redis.Eval(ctx, actorLocatorClaimScript, []string{
		k.locateKey(),
		actorLocatorLeaseKey(l.nodeID),
		actorLocatorEpochKey(),
	}, l.nodeID, l.leaseToken).Result()
	if err != nil {
		return ZeroActorOwner, errors.Wrap(err, "claim actor owner")
	}
	if !l.leaseValid(time.Now()) {
		return ZeroActorOwner, errActorLocatorLeaseInvalid
	}
	parts, ok := result.([]any)
	if !ok || len(parts) != 2 {
		return ZeroActorOwner, errors.Newf("unexpected actor claim result: %T", result)
	}
	sstatus, ok := redisString(parts[0])
	if !ok {
		return ZeroActorOwner, errors.New("actor claim result status is not a string")
	}
	status := claimResult(sstatus)
	if status == claimInvalidLease {
		return ZeroActorOwner, errActorLocatorLeaseInvalid
	}

	ownerValue, ok := redisString(parts[1])
	if !ok {
		return ZeroActorOwner, errors.New("actor claim result owner is not a string")
	}
	switch status {
	case claimAcquired:
		owner, err := decodeActorOwner(ownerValue)
		if err != nil {
			return ZeroActorOwner, err
		}
		return owner, nil
	case claimAlreadyOwn, claimOwnedOther:
		// 记录在别的节点,或本节点已有记录:正常的竞争结局,不是故障。
		// 具体是哪种写进错误便于诊断;两者对调用方的处置相同——本次没拿到。
		return ZeroActorOwner, errors.Wrapf(ErrNotOwner, "actor %s (claim %s)", k, status)
	default:
		return ZeroActorOwner, errors.Newf("unknown actor claim result: %s", status)
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

// Locate 查询归属,满足 OwnershipStore。
func (l *actorLocator) Locate(ctx context.Context, kind string, id string) (ActorOwner, error) {
	k := actorKey{kind: kind, id: id}
	if err := l.ensureClient(); err != nil {
		return ZeroActorOwner, err
	}
	value, err := l.redis.Eval(ctx, actorLocatorLocateScript, []string{k.locateKey()}).Result()
	if err != nil {
		return ZeroActorOwner, errors.Wrap(err, "locate actor owner")
	}
	ownerValue, ok := redisString(value)
	if !ok {
		return ZeroActorOwner, errors.Newf("unexpected actor locate result: %T", value)
	}
	if ownerValue == "" {
		return ZeroActorOwner, nil
	}
	return decodeActorOwner(ownerValue)
}

// Release 条件释放,满足 OwnershipStore。
func (l *actorLocator) Release(ctx context.Context, kind string, id string, owner ActorOwner) (bool, error) {
	k := actorKey{kind: kind, id: id}
	if err := l.ensureClient(); err != nil {
		return false, err
	}
	result, err := l.redis.Eval(ctx, actorLocatorReleaseScript, []string{k.locateKey()}, encodeActorOwner(owner, l.leaseToken)).Int64()
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

// leaseHeartbeat 是一次节点租约心跳循环:周期性续租,续租失败或 deadline 到期即自栅(fence)。
//
// 状态显式放在结构体上(而非闭包捕获),使 select 的每个分支成为一个方法。
type leaseHeartbeat struct {
	locator *actorLocator
	ctx     context.Context
	onLost  func(error)
	renew   func(context.Context) (bool, error)

	ticker        *time.Ticker
	deadlineTimer *time.Timer
	renewResults  chan leaseRenewResult
	renewing      bool
	renewCancel   context.CancelFunc
	lastErr       error
}

func (l *actorLocator) startLeaseHeartbeat(ctx context.Context, onLost func(error)) func() {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	renew := l.renewNodeLease
	if l.renewLease != nil {
		renew = l.renewLease
	}
	h := &leaseHeartbeat{
		locator:       l,
		ctx:           heartbeatCtx,
		onLost:        onLost,
		renew:         renew,
		ticker:        time.NewTicker(l.heartbeatInterval),
		deadlineTimer: time.NewTimer(time.Until(time.Unix(0, l.leaseDeadline.Load()))),
		renewResults:  make(chan leaseRenewResult, 1),
	}

	go func() {
		defer close(done)
		defer h.ticker.Stop()
		defer h.deadlineTimer.Stop()
		h.run()
	}()

	return func() {
		cancel()
		<-done
	}
}

func (h *leaseHeartbeat) run() {
	for {
		select {
		case <-h.ctx.Done():
			h.cancelRenew()
			return
		case <-h.deadlineTimer.C:
			h.fence(h.deadlineError())
			return
		case <-h.ticker.C:
			h.startRenew()
		case result := <-h.renewResults:
			if h.finishRenew(result) {
				return
			}
		}
	}
}

func (h *leaseHeartbeat) cancelRenew() {
	if h.renewCancel != nil {
		h.renewCancel()
	}
}

func (h *leaseHeartbeat) deadlineError() error {
	if h.lastErr != nil {
		return errors.Wrap(h.lastErr, errActorLocatorLeaseDeadline.Error())
	}
	return errActorLocatorLeaseDeadline
}

func (h *leaseHeartbeat) fence(err error) {
	h.locator.fenced.Store(true)
	h.cancelRenew()
	h.onLost(err)
}

// startRenew 发起一次异步续租(已有续租在飞则跳过)。
func (h *leaseHeartbeat) startRenew() {
	if h.renewing {
		return
	}
	h.renewing = true
	started := time.Now()
	renewCtx, cancelRenew := context.WithCancel(h.ctx)
	h.renewCancel = cancelRenew
	go func() {
		refreshed, err := h.renew(renewCtx)
		result := leaseRenewResult{refreshed: refreshed, err: err, started: started, finished: time.Now()}
		select {
		case h.renewResults <- result:
		case <-h.ctx.Done():
		}
	}()
}

// finishRenew 处理一次续租结果,返回是否要终止心跳。
func (h *leaseHeartbeat) finishRenew(result leaseRenewResult) bool {
	h.renewing = false
	h.renewCancel()
	h.renewCancel = nil
	if result.finished.UnixNano() >= h.locator.leaseDeadline.Load() {
		h.fence(errActorLocatorLeaseDeadline)
		return true
	}
	if result.err != nil {
		h.lastErr = result.err
		gxylog.Warn(h.ctx, "renew actor node lease failed",
			gxylog.Str("node", h.locator.nodeID), gxylog.Err(result.err))
		return false
	}
	if !result.refreshed {
		h.fence(errActorLocatorLeaseInvalid)
		return true
	}
	h.locator.confirmLease(result.started)
	h.lastErr = nil
	if !h.deadlineTimer.Stop() {
		select {
		case <-h.deadlineTimer.C:
		default:
		}
	}
	h.deadlineTimer.Reset(time.Until(time.Unix(0, h.locator.leaseDeadline.Load())))
	return false
}

func encodeActorOwner(owner ActorOwner, leaseToken string) string {
	return strings.Join([]string{owner.NodeID, strconv.FormatUint(owner.Epoch, 10), leaseToken}, actorLocateOwnerSeparator)
}

func decodeActorOwner(value string) (ActorOwner, error) {
	parts := strings.SplitN(value, actorLocateOwnerSeparator, 3)
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
		return ZeroActorOwner, errors.Newf("invalid actor owner value: %q", value)
	}
	epoch, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return ZeroActorOwner, errors.Wrapf(err, "parse actor owner epoch %q", value)
	}
	if epoch == 0 {
		return ZeroActorOwner, errors.Newf("invalid actor owner epoch: %q", value)
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
