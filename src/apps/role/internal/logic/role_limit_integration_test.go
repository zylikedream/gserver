package logic

import (
	"context"
	"testing"

	"gserver/core/gxylimit"
	"gserver/core/gxymetrics"
	"gserver/protocol/pb"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// limitTestConfig 返回集成测试用的完整配置: RoleBasic/RoleFlower 使用独立 rate,
// 供脚本化桶工厂按 rate 区分; 其余模块沿用默认策略(Rate=10)。
func limitTestConfig() RoleLimitConfig {
	cfg := testRoleLimitConfig()
	cfg.Modules["RoleBasic"] = ModuleLimitPolicy{Rate: 1, Burst: 1}
	cfg.Modules["RoleFlower"] = ModuleLimitPolicy{Rate: 2, Burst: 2}
	return cfg
}

// scriptedLimitFactory 按 rate 分发脚本化桶, 未登记的 rate 视为测试配置错误。
func scriptedLimitFactory(t *testing.T, buckets map[float64]*scriptedBucket) bucketFactory {
	t.Helper()
	return func(cfg gxylimit.Config) (tokenBucket, error) {
		b, ok := buckets[cfg.Rate]
		if !ok {
			t.Fatalf("unexpected bucket config rate=%v burst=%d", cfg.Rate, cfg.Burst)
			return nil, nil
		}
		return b, nil
	}
}

// newRoleMainForLimitTest 构造可直接调用 HandleClientMsg 的 RoleMain:
// 设置 RoleID 与登录态、初始化子模块、注入脚本化桶工厂, 并要求 initMsgHandler 成功。
func newRoleMainForLimitTest(t *testing.T, config RoleLimitConfig, factory bucketFactory) *RoleMain {
	t.Helper()
	t.Cleanup(swapRoleLimitConfig(config))
	r := NewRoleMain()
	r.RoleID = 1001
	r.state = RoleStateLogined
	r.SetSelfMod(r)
	r.initRoleModules(context.Background())
	for _, mod := range r.Modules() {
		rmod, ok := mod.(IRoleModule)
		if !ok {
			t.Fatalf("module %T is not IRoleModule", mod)
		}
		rmod.SetRole(r)
	}
	r.newBucket = factory
	if err := r.initMsgHandler(); err != nil {
		t.Fatalf("initMsgHandler: %v", err)
	}
	return r
}

// clientMsg 把请求消息打包成 HandleClientMsg 的入参。
func clientMsg(t *testing.T, id string, msg proto.Message) *pb.ClientMsg {
	t.Helper()
	anyMsg, err := anypb.New(msg)
	if err != nil {
		t.Fatalf("anypb.New: %v", err)
	}
	return &pb.ClientMsg{Id: id, Msg: anyMsg}
}

// unpackServerMsg 解包 ServerMsg 并返回内部业务消息。
func unpackServerMsg(t *testing.T, res proto.Message) proto.Message {
	t.Helper()
	svr, ok := res.(*pb.ServerMsg)
	if !ok {
		t.Fatalf("response type = %T, want *pb.ServerMsg", res)
	}
	inner, err := anypb.UnmarshalNew(svr.Msg, proto.UnmarshalOptions{})
	if err != nil {
		t.Fatalf("unmarshal server msg: %v", err)
	}
	return inner
}

func admissionCounter(module, result string) float64 {
	return testutil.ToFloat64(gxymetrics.RoleModuleLimitTotal.WithLabelValues(module, result))
}

func clientRequestCounter(id string, msg proto.Message, result string) float64 {
	msgID, msgName := clientMessageMetricLabels(id, msg)
	return testutil.ToFloat64(gxymetrics.ClientRequests.WithLabelValues(msgID, msgName, result))
}

func TestRoleLimitIntegration_HandlerMap(t *testing.T) {
	r := newRoleMainForLimitTest(t, limitTestConfig(), scriptedLimitFactory(t, map[float64]*scriptedBucket{
		1:  {allowed: []bool{true}},
		2:  {allowed: []bool{true}},
		10: {allowed: []bool{true}},
	}))

	if got := r.moduleByMessage["ReqBasicInfo"]; got != "RoleBasic" {
		t.Fatalf("moduleByMessage[ReqBasicInfo] = %q, want RoleBasic", got)
	}
	if got := r.moduleByMessage["ReqFlowerInfo"]; got != "RoleFlower" {
		t.Fatalf("moduleByMessage[ReqFlowerInfo] = %q, want RoleFlower", got)
	}
	for _, name := range []string{"ReqAccountLogin", "ReqAccountLogout"} {
		if _, ok := r.moduleByMessage[name]; ok {
			t.Fatalf("moduleByMessage[%s] present, want absent", name)
		}
	}
}

func TestRoleLimitIntegration_RateLimited(t *testing.T) {
	basic := &scriptedBucket{allowed: []bool{false}}
	r := newRoleMainForLimitTest(t, limitTestConfig(), scriptedLimitFactory(t, map[float64]*scriptedBucket{
		1:  basic,
		2:  {allowed: []bool{true}},
		10: {allowed: []bool{true}},
	}))

	reqID := "req-1"
	limitedBefore := admissionCounter("RoleBasic", "limited")
	reqBefore := clientRequestCounter(reqID, &pb.ReqBasicInfo{}, "limited")

	res, err := r.HandleClientMsg(context.Background(), clientMsg(t, reqID, &pb.ReqBasicInfo{}))
	if err != nil {
		t.Fatalf("HandleClientMsg: %v", err)
	}
	ack, ok := unpackServerMsg(t, res).(*pb.Ack)
	if !ok {
		t.Fatalf("inner = %T, want *pb.Ack (RATE_LIMITED), not RspBasicInfo", res)
	}
	if ack.Code != pb.AckCode_ACK_CODE_RATE_LIMITED {
		t.Fatalf("ack code = %v, want ACK_CODE_RATE_LIMITED", ack.Code)
	}
	if ack.Id != reqID {
		t.Fatalf("ack id = %q, want %q", ack.Id, reqID)
	}
	if ack.Reason != "rate limited" {
		t.Fatalf("ack reason = %q, want %q", ack.Reason, "rate limited")
	}
	if r.state != RoleStateLogined {
		t.Fatalf("state = %v, want RoleStateLogined (session kept)", r.state)
	}
	if basic.calls != 1 {
		t.Fatalf("basic bucket calls = %d, want 1", basic.calls)
	}
	if got := admissionCounter("RoleBasic", "limited") - limitedBefore; got != 1 {
		t.Fatalf("RoleModuleLimitTotal{RoleBasic,limited} delta = %v, want 1", got)
	}
	if got := clientRequestCounter(reqID, &pb.ReqBasicInfo{}, "limited") - reqBefore; got != 1 {
		t.Fatalf("ClientRequests{ReqBasicInfo,limited} delta = %v, want 1", got)
	}
}

func TestRoleLimitIntegration_Disabled(t *testing.T) {
	cfg := limitTestConfig()
	policy := cfg.Modules["RoleBasic"]
	policy.Disabled = true
	cfg.Modules["RoleBasic"] = policy

	// RoleBasic 停用 → 工厂不会被要求创建它的桶。
	r := newRoleMainForLimitTest(t, cfg, scriptedLimitFactory(t, map[float64]*scriptedBucket{
		2:  {allowed: []bool{true}},
		10: {allowed: []bool{true}},
	}))

	reqID := "req-2"
	disabledBefore := admissionCounter("RoleBasic", "disabled")

	res, err := r.HandleClientMsg(context.Background(), clientMsg(t, reqID, &pb.ReqBasicInfo{}))
	if err != nil {
		t.Fatalf("HandleClientMsg: %v", err)
	}
	ack, ok := unpackServerMsg(t, res).(*pb.Ack)
	if !ok {
		t.Fatalf("inner = %T, want *pb.Ack (MODULE_DISABLED)", res)
	}
	if ack.Code != pb.AckCode_ACK_CODE_MODULE_DISABLED {
		t.Fatalf("ack code = %v, want ACK_CODE_MODULE_DISABLED", ack.Code)
	}
	if ack.Id != reqID {
		t.Fatalf("ack id = %q, want %q", ack.Id, reqID)
	}
	if ack.Reason != "module disabled" {
		t.Fatalf("ack reason = %q, want %q", ack.Reason, "module disabled")
	}
	if r.state != RoleStateLogined {
		t.Fatalf("state = %v, want RoleStateLogined (session kept)", r.state)
	}
	if got := admissionCounter("RoleBasic", "disabled") - disabledBefore; got != 1 {
		t.Fatalf("RoleModuleLimitTotal{RoleBasic,disabled} delta = %v, want 1", got)
	}
}

func TestRoleLimitIntegration_PermittedReachesHandler(t *testing.T) {
	basic := &scriptedBucket{allowed: []bool{true}}
	r := newRoleMainForLimitTest(t, limitTestConfig(), scriptedLimitFactory(t, map[float64]*scriptedBucket{
		1:  basic,
		2:  {allowed: []bool{true}},
		10: {allowed: []bool{true}},
	}))

	reqID := "req-3"
	okBefore := admissionCounter("RoleBasic", "ok")

	res, err := r.HandleClientMsg(context.Background(), clientMsg(t, reqID, &pb.ReqBasicInfo{}))
	if err != nil {
		t.Fatalf("HandleClientMsg: %v", err)
	}
	rsp, ok := unpackServerMsg(t, res).(*pb.RspBasicInfo)
	if !ok {
		t.Fatalf("inner = %T, want *pb.RspBasicInfo (real handler response)", res)
	}
	if rsp.RoleId != r.RoleID {
		t.Fatalf("rsp.RoleId = %d, want %d", rsp.RoleId, r.RoleID)
	}
	// 放行恰好消费一个令牌, 请求确实到达真实 Handler。
	if basic.calls != 1 {
		t.Fatalf("basic bucket calls = %d, want 1", basic.calls)
	}
	if got := admissionCounter("RoleBasic", "ok") - okBefore; got != 1 {
		t.Fatalf("RoleModuleLimitTotal{RoleBasic,ok} delta = %v, want 1", got)
	}
}
