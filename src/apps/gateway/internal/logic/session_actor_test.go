package logic

// Session 会话行为测试:同包白盒 + fakeActx + fakeEndpoint,
// 覆盖握手、客户端/服务端消息路由、空闲检测、断连与终止流程。

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxymetrics"
	"gserver/core/gxynet/message"
	"gserver/core/gxytimer"
	"gserver/core/gxyutil"
	"gserver/protocol/pb"
	"gserver/src/lib/gatetoken"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/anypb"
)

// TestMain 初始化全局 actor app(不建 system)+ SessionMgr,
// 使 Send/LocalSend 走 "node not initialized" 错误路径而非 nil panic。
func TestMain(m *testing.M) {
	NewSessionMgr()
	// 测试默认使用允许型登录准入器：旧握手测试显式、且不削弱生产 fail-closed 默认值
	currentLoginAcquirer = &stubLoginAcquirer{permit: noopLoginPermit{}}
	os.Exit(m.Run())
}

// fakeEndpoint 记录发送/关闭行为; Conn() 返回 net.Pipe 一端(RemoteAddr 可用)。
type fakeEndpoint struct {
	sentMsgs []any
	sendErr  error
	closed   bool
	data     any
	conn     net.Conn
}

func newFakeEndpoint(t *testing.T) *fakeEndpoint {
	t.Helper()
	c1, c2 := net.Pipe()
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })
	return &fakeEndpoint{conn: c1}
}

func (f *fakeEndpoint) SendData(data []byte, path string) error { return nil }
func (f *fakeEndpoint) SendMsg(msg any) error {
	f.sentMsgs = append(f.sentMsgs, msg)
	return f.sendErr
}
func (f *fakeEndpoint) Conn() net.Conn { return f.conn }
func (f *fakeEndpoint) Close()         { f.closed = true }
func (f *fakeEndpoint) GetData() any   { return f.data }
func (f *fakeEndpoint) SetData(d any)  { f.data = d }

type fakeActx struct {
	context.Context
	self      gxyactor.PID
	sender    gxyactor.PID
	timer     *gxyactor.ActorTimer
	stopErr   error
	watched   []gxyactor.PID
	unwatched []gxyactor.PID
}

func (f *fakeActx) Self() gxyactor.PID                               { return f.self }
func (f *fakeActx) Sender() gxyactor.PID                             { return f.sender }
func (f *fakeActx) Stop(err error)                                   { f.stopErr = err }
func (f *fakeActx) Watch(pid gxyactor.PID)                           { f.watched = append(f.watched, pid) }
func (f *fakeActx) Unwatch(pid gxyactor.PID)                         { f.unwatched = append(f.unwatched, pid) }
func (*fakeActx) Children() []gxyactor.PID                           { return nil }
func (f *fakeActx) Timer() *gxyactor.ActorTimer                      { return f.timer }
func (f *fakeActx) Span() trace.Span                                 { return trace.SpanFromContext(f) }
func (*fakeActx) SetLogValue(string, any)                            {}
func (*fakeActx) AddMsgHandler(any, ...string) []*gxyutil.MethodMeta { return nil }
func (*fakeActx) AutoHandleMsg(any) (any, error)                     { return nil, nil }
func (*fakeActx) Respond(any, ...error) error                        { return nil }

var fake = &fakeActx{
	Context: context.Background(),
	self:    gxyactor.PID{Runtime: "test", Node: "node", ID: "test_session", Creation: "1"},
	sender:  gxyactor.PID{Runtime: "test", Node: "node", ID: "sender", Creation: "1"},
}

func newTestSession(t *testing.T) (*Session, *fakeActx, *fakeEndpoint) {
	t.Helper()
	ep := newFakeEndpoint(t)
	fake := &fakeActx{
		Context: context.Background(),
		self:    gxyactor.PID{Runtime: "test", Node: "node", ID: "test_session", Creation: "1"},
		sender:  gxyactor.PID{Runtime: "test", Node: "node", ID: "sender", Creation: "1"},
	}
	fake.timer = gxyactor.NewActorTimer(fake.self)
	s := NewSession(ep)
	if err := s.Init(fake, nil); err != nil {
		t.Fatal(err)
	}
	return s, fake, ep
}

// withHandshake 完成一次成功握手:替换 token 验证、登录准入与角色激活, 返回可恢复函数。
func withHandshake(t *testing.T, s *Session, fake *fakeActx, ep *fakeEndpoint) func() {
	t.Helper()
	restoreToken := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
		return &gatetoken.Claims{AccountID: "acc_1", RoleID: 10001}, nil
	})
	restoreAcquirer := swapLoginAcquirer(&stubLoginAcquirer{permit: noopLoginPermit{}})
	oldActivate := activateRole
	activateRole = func(ctx context.Context, roleID int64) (gxyactor.PID, error) {
		return gxyactor.PID{Runtime: "test", Node: "node", ID: "role_pid", Creation: "1"}, nil
	}
	oldMaint := gateMaintenanceEnabled
	gateMaintenanceEnabled = func() bool { return false }
	err := s.handleHandshake(fake, &pb.ReqHandShake{GateToken: "ok"})
	if err != nil {
		restoreToken()
		restoreAcquirer()
		activateRole = oldActivate
		gateMaintenanceEnabled = oldMaint
		t.Fatalf("handshake: %v", err)
	}
	return func() {
		restoreToken()
		restoreAcquirer()
		activateRole = oldActivate
		gateMaintenanceEnabled = oldMaint
	}
}

// ========== sessionDisconnectReason ==========

func TestSessionDisconnectReason(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, "unknown"},
		{ErrLoginRateLimited, "login_rate_limited"},
		{ErrLoginQueueFull, "login_queue_full"},
		{ErrLoginQueueTimeout, "login_queue_timeout"},
		{gerror.New("client account logout"), "client_logout"},
		{gerror.New("client idle timeout"), "client_idle_timeout"},
		{gerror.New("server idle timeout"), "server_idle_timeout"},
		{gerror.New("role terminated"), "role_terminated"},
		{gerror.New("multi login"), "multi_login"},
		{gerror.New("gateway service stop"), "service_stop"},
		{gerror.New("conn closed"), "conn_closed"},
		{gerror.New("some other error"), "error"},
	}
	for _, c := range cases {
		if got := sessionDisconnectReason(c.err); got != c.want {
			t.Fatalf("sessionDisconnectReason(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

// ========== Init / DelayInit ==========

func TestSession_Init(t *testing.T) {
	s, _, _ := newTestSession(t)
	if s.state != StateConnected {
		t.Fatalf("expected StateConnected, got %v", s.state)
	}
	info := s.GetSessionInfo()
	if info.ConnectTime.IsZero() || info.ClientLastActive.IsZero() {
		t.Fatalf("session times not initialized: %+v", info)
	}
}

func TestSession_DelayInit(t *testing.T) {
	s, fake, _ := newTestSession(t)
	if err := s.DelayInit(fake); err != nil {
		t.Fatalf("DelayInit: %v", err)
	}
	info := s.GetSessionInfo()
	if info.ServerLastActive.IsZero() {
		t.Fatal("ServerLastActive not updated by DelayInit")
	}
}

// ========== HandleMessage 路由 ==========

func TestSession_HandleMessage_ClientMsg(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, fake, ep)
	defer restore()
	ep.sentMsgs = nil

	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: &pb.RspAccountLogin{}}
	if err := s.HandleMessage(fake, msg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
}

func TestSession_HandleMessage_ServerMsg(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, fake, ep)
	defer restore()
	ep.sentMsgs = nil

	anyMsg, err := anypb.New(&pb.RspAccountLogin{})
	if err != nil {
		t.Fatalf("anypb.New: %v", err)
	}
	serverMsg := &pb.ServerMsg{Msg: anyMsg}
	if err := s.HandleMessage(fake, serverMsg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if s.state != StateLogin {
		t.Fatalf("expected StateLogin after RspAccountLogin, got %v", s.state)
	}
}

func TestSession_HandleMessage_RoleTerminated(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, fake, ep)
	defer restore()

	_ = s.HandleMessage(fake, gxyactor.ActorTerminatedMessage{Who: gxyactor.PID{Runtime: "test", Node: "node", ID: "role_pid", Creation: "1"}})
	if fake.stopErr == nil {
		t.Fatal("expected Stop after role terminated")
	}
	if !s.sessionInfo.RolePid.IsZero() {
		t.Fatal("RolePid should be cleared")
	}
}

func TestSession_HandleMessage_RoleTerminated_OtherPid(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, fake, ep)
	defer restore()

	_ = s.HandleMessage(fake, gxyactor.ActorTerminatedMessage{Who: gxyactor.PID{Runtime: "test", Node: "node", ID: "other", Creation: "1"}})
	if fake.stopErr != nil {
		t.Fatal("unrelated Terminated should not stop session")
	}
	if s.sessionInfo.RolePid.IsZero() {
		t.Fatal("RolePid should remain")
	}
}

func TestSession_HandleMessage_ActorError(t *testing.T) {
	s, fake, _ := newTestSession(t)
	_ = s.HandleMessage(fake, &pb.ActorError{Reason: "boom"})
	if fake.stopErr == nil {
		t.Fatal("expected Stop after ActorError")
	}
}

// ========== 握手 ==========

func TestSession_Handshake_NotHandshakeMsg(t *testing.T) {
	s, _, _ := newTestSession(t)
	if err := s.handleHandshake(fake, &pb.ReqChannelSend{}); err == nil {
		t.Fatal("expected error for non-handshake msg")
	}
}

func TestSession_Handshake_Maintenance(t *testing.T) {
	s, _, _ := newTestSession(t)
	old := gateMaintenanceEnabled
	gateMaintenanceEnabled = func() bool { return true }
	defer func() { gateMaintenanceEnabled = old }()

	if err := s.handleHandshake(fake, &pb.ReqHandShake{GateToken: "x"}); err == nil {
		t.Fatal("expected maintenance error")
	}
}

func TestSession_Handshake_EmptyToken(t *testing.T) {
	s, _, _ := newTestSession(t)
	if err := s.handleHandshake(fake, &pb.ReqHandShake{GateToken: ""}); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestSession_Handshake_ActivateRoleFailed(t *testing.T) {
	s, _, _ := newTestSession(t)
	restore := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
		return &gatetoken.Claims{AccountID: "a", RoleID: 1}, nil
	})
	defer restore()
	old := activateRole
	activateRole = func(ctx context.Context, roleID int64) (gxyactor.PID, error) {
		return gxyactor.PID{}, gerror.New("activate failed")
	}
	defer func() { activateRole = old }()

	if err := s.handleHandshake(fake, &pb.ReqHandShake{GateToken: "ok"}); err == nil {
		t.Fatal("expected activate error")
	}
}

func TestSession_Handshake_Success(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, fake, ep)
	defer restore()

	if s.state != StateHandshake {
		t.Fatalf("expected StateHandshake, got %v", s.state)
	}
	info := s.GetSessionInfo()
	if info.AccountID != "acc_1" || info.RoleID != 10001 {
		t.Fatalf("unexpected session info: %+v", info)
	}
	if len(fake.watched) != 1 || fake.watched[0].ID != "role_pid" {
		t.Fatalf("expected Watch(role_pid), got %+v", fake.watched)
	}
	if len(ep.sentMsgs) != 1 {
		t.Fatalf("expected 1 handshake rsp, got %d", len(ep.sentMsgs))
	}
	if _, ok := ep.sentMsgs[0].(*pb.RspHandShake); !ok {
		t.Fatalf("expected RspHandShake, got %T", ep.sentMsgs[0])
	}
	if SessionMgr().Count() != 1 {
		t.Fatalf("expected 1 session in mgr, got %d", SessionMgr().Count())
	}
}

// ========== 登录准入 ==========

// TestSession_LoginAdmission_Rejections 每个预期准入拒绝都必须:
// 阻止 activateRole、使 OnHandleClientMessage 停止 Session 并向 Actor 边界返回 nil;
// 且 sentinel 错误对象原样到达 Terminate, 映射为固定断连标签。
func TestSession_LoginAdmission_Rejections(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		label string
	}{
		{"rate_limited", ErrLoginRateLimited, "login_rate_limited"},
		{"queue_full", ErrLoginQueueFull, "login_queue_full"},
		{"queue_timeout", ErrLoginQueueTimeout, "login_queue_timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, fake, ep := newTestSession(t)
			before := testutil.ToFloat64(gxymetrics.SessionDisconnects.WithLabelValues(tc.label))
			restore := swapLoginAcquirer(&stubLoginAcquirer{err: tc.err})
			defer restore()
			restoreToken := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
				return &gatetoken.Claims{AccountID: "acc_1", RoleID: 10001}, nil
			})
			defer restoreToken()
			oldActivate := activateRole
			activateRole = func(ctx context.Context, roleID int64) (gxyactor.PID, error) {
				t.Fatal("activateRole must not be called on admission rejection")
				return gxyactor.PID{}, nil
			}
			defer func() { activateRole = oldActivate }()

			msg := &message.Message{
				Type: message.MESSGE_TYPE_FIRST_PACKET,
				Msg:  &pb.ReqHandShake{GateToken: "ok"},
			}
			if err := s.OnHandleClientMessage(fake, msg); err != nil {
				t.Fatalf("expected nil at actor boundary, got: %v", err)
			}
			if fake.stopErr == nil {
				t.Fatal("expected Stop after admission rejection")
			}
			if s.state != StateConnected {
				t.Fatalf("session state = %v, want StateConnected", s.state)
			}
			if !s.sessionInfo.RolePid.IsZero() {
				t.Fatal("RolePid must stay nil after admission rejection")
			}
			s.Terminate(fake, fake.stopErr)
			if !ep.closed {
				t.Fatal("expected endpoint closed after Terminate")
			}
			if s.state != StateDisconnected {
				t.Fatalf("session state = %v, want StateDisconnected", s.state)
			}
			if got := testutil.ToFloat64(gxymetrics.SessionDisconnects.WithLabelValues(tc.label)); got != before+1 {
				t.Fatalf("disconnect counter(%q) = %v, want %v", tc.label, got, before+1)
			}
		})
	}
}

// TestSession_LoginAdmission_SuccessHoldsThenReleases 成功准入时:
// permit 在 activateRole 执行期间仍被持有, handleHandshake 返回前恰好释放一次。
func TestSession_LoginAdmission_SuccessHoldsThenReleases(t *testing.T) {
	s, fake, ep := newTestSession(t)
	permit := &recordingLoginPermit{}
	restore := swapLoginAcquirer(&stubLoginAcquirer{permit: permit})
	defer restore()
	restoreToken := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
		return &gatetoken.Claims{AccountID: "acc_1", RoleID: 10001}, nil
	})
	defer restoreToken()
	oldActivate := activateRole
	var heldDuringActivate bool
	activateRole = func(ctx context.Context, roleID int64) (gxyactor.PID, error) {
		heldDuringActivate = permit.releases == 0
		return gxyactor.PID{Runtime: "test", Node: "node", ID: "role_pid", Creation: "1"}, nil
	}
	defer func() { activateRole = oldActivate }()

	if err := s.handleHandshake(fake, &pb.ReqHandShake{GateToken: "ok"}); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if !heldDuringActivate {
		t.Fatal("permit was not held while activateRole ran")
	}
	if permit.releases != 1 {
		t.Fatalf("permit released %d times, want exactly 1", permit.releases)
	}
	if len(fake.watched) != 1 || fake.watched[0].ID != "role_pid" {
		t.Fatalf("expected Watch(role_pid) after release, got %+v", fake.watched)
	}
	if len(ep.sentMsgs) != 1 {
		t.Fatalf("expected 1 handshake rsp after release, got %d", len(ep.sentMsgs))
	}
}

// TestSession_LoginAdmission_ActivateRoleErrorReleases activateRole 失败时仍恰好释放一次。
func TestSession_LoginAdmission_ActivateRoleErrorReleases(t *testing.T) {
	s, _, _ := newTestSession(t)
	permit := &recordingLoginPermit{}
	restore := swapLoginAcquirer(&stubLoginAcquirer{permit: permit})
	defer restore()
	restoreToken := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
		return &gatetoken.Claims{AccountID: "acc_1", RoleID: 10001}, nil
	})
	defer restoreToken()
	oldActivate := activateRole
	activateRole = func(ctx context.Context, roleID int64) (gxyactor.PID, error) {
		return gxyactor.PID{}, gerror.New("activate failed")
	}
	defer func() { activateRole = oldActivate }()

	if err := s.handleHandshake(fake, &pb.ReqHandShake{GateToken: "ok"}); err == nil {
		t.Fatal("expected activateRole error")
	}
	if permit.releases != 1 {
		t.Fatalf("permit released %d times, want exactly 1", permit.releases)
	}
}

// TestSession_LoginAdmission_UnconfiguredPropagates 未配置限流器属于启动/接线缺陷,
// 不应被归类为预期拒绝: 错误必须传播到 Actor 边界, 且不停止 Session。
func TestSession_LoginAdmission_UnconfiguredPropagates(t *testing.T) {
	s, fake, _ := newTestSession(t)
	restore := swapLoginAcquirer(&stubLoginAcquirer{err: ErrLoginLimiterUnconfigured})
	defer restore()
	restoreToken := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
		return &gatetoken.Claims{AccountID: "acc_1", RoleID: 10001}, nil
	})
	defer restoreToken()
	oldActivate := activateRole
	activateRole = func(ctx context.Context, roleID int64) (gxyactor.PID, error) {
		t.Fatal("activateRole must not be called when limiter is unconfigured")
		return gxyactor.PID{}, nil
	}
	defer func() { activateRole = oldActivate }()

	msg := &message.Message{
		Type: message.MESSGE_TYPE_FIRST_PACKET,
		Msg:  &pb.ReqHandShake{GateToken: "ok"},
	}
	err := s.OnHandleClientMessage(fake, msg)
	if err == nil {
		t.Fatal("expected unconfigured error to propagate to actor boundary")
	}
	if isLoginAdmissionRejection(err) {
		t.Fatalf("unconfigured must not be classified as an expected rejection, got: %v", err)
	}
	if !strings.Contains(err.Error(), "login limiter not configured") {
		t.Fatalf("propagated error = %v, want ErrLoginLimiterUnconfigured", err)
	}
	if fake.stopErr != nil {
		t.Fatal("unconfigured wiring defect must not stop the session")
	}
}

// ========== 客户端消息 ==========

func TestSession_ClientMessage_Logout(t *testing.T) {
	s, fake, _ := newTestSession(t)
	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: &pb.ReqAccountLogout{}}
	if err := s.OnHandleClientMessage(fake, msg); err != nil {
		t.Fatalf("OnHandleClientMessage: %v", err)
	}
	if fake.stopErr == nil {
		t.Fatal("expected Stop after client logout")
	}
}

func TestSession_ClientMessage_NotProto(t *testing.T) {
	s, _, _ := newTestSession(t)
	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: "not a proto"}
	if err := s.OnHandleClientMessage(fake, msg); err == nil {
		t.Fatal("expected error for non-proto msg")
	}
}

func TestSession_ClientMessage_DataPacket_NoRolePid(t *testing.T) {
	s, _, _ := newTestSession(t) // 未握手, RolePid nil
	// SendRoleMsg 在测试环境没有 runtime，发送错误被忽略，保持 fire-and-forget 契约。
	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: &pb.RspAccountLogin{}}
	if err := s.OnHandleClientMessage(fake, msg); err != nil {
		t.Fatalf("OnHandleClientMessage: %v", err)
	}
}

// ========== 服务端消息 ==========

func TestSession_ServerMessage_BadAny(t *testing.T) {
	s, _, _ := newTestSession(t)
	bad := &anypb.Any{TypeUrl: "garbage", Value: []byte{0xff}}
	if err := s.OnHandleServerMessage(fake, &pb.ServerMsg{Msg: bad}); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestSession_ServerMessage_DisconnectedSkipsSend(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, fake, ep)
	defer restore()
	ep.sentMsgs = nil
	s.state = StateDisconnected

	// RspAccountLogin 会重置 state=Login, 用普通响应验证 Disconnected 跳过
	anyMsg, _ := anypb.New(&pb.RspHandShake{})
	if err := s.OnHandleServerMessage(fake, &pb.ServerMsg{Msg: anyMsg}); err != nil {
		t.Fatalf("OnHandleServerMessage: %v", err)
	}
	if len(ep.sentMsgs) != 0 {
		t.Fatalf("disconnected session must not send, got %d msgs", len(ep.sentMsgs))
	}
}

// ========== sendClientMsg ==========

func TestSession_SendClientMsg_Success(t *testing.T) {
	s, _, ep := newTestSession(t)
	if err := s.sendClientMsg(fake, &pb.RspHandShake{}); err != nil {
		t.Fatalf("sendClientMsg: %v", err)
	}
	if len(ep.sentMsgs) != 1 {
		t.Fatalf("expected 1 sent msg, got %d", len(ep.sentMsgs))
	}
}

func TestSession_SendClientMsg_FailureStopsSession(t *testing.T) {
	s, fake, ep := newTestSession(t)
	ep.sendErr = gerror.New("conn broken")
	if err := s.sendClientMsg(fake, &pb.RspHandShake{}); err != nil {
		t.Fatalf("sendClientMsg should swallow send error, got %v", err)
	}
	if fake.stopErr == nil {
		t.Fatal("expected Stop after send failure")
	}
}

// ========== 空闲检测 ==========

func TestSession_SessionCheck_ClientIdle(t *testing.T) {
	s, fake, _ := newTestSession(t)
	s.sessionInfo.ClientLastActive = time.Now().Add(-SESSION_CLIENT_IDLE_TIMEOUT - time.Minute)
	s.sessionCheck(fake, gxytimer.TimerActiveInfo{})
	if fake.stopErr == nil {
		t.Fatal("expected Stop after client idle")
	}
}

func TestSession_SessionCheck_ServerIdle(t *testing.T) {
	s, fake, _ := newTestSession(t)
	s.sessionInfo.ServerLastActive = time.Now().Add(-SESSION_SERVER_IDLE_TIMEOUT - time.Minute)
	s.sessionCheck(fake, gxytimer.TimerActiveInfo{})
	if fake.stopErr == nil {
		t.Fatal("expected Stop after server idle")
	}
}

func TestSession_SessionCheck_Active(t *testing.T) {
	s, fake, _ := newTestSession(t)
	s.updateClientLastActive()
	s.updateServerLastActive()
	s.sessionCheck(fake, gxytimer.TimerActiveInfo{})
	if fake.stopErr != nil {
		t.Fatal("active session must not stop")
	}
}

// ========== Terminate ==========

func TestSession_Terminate_WithRole(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, fake, ep)
	defer restore()

	s.Terminate(fake, gerror.New("client account logout"))
	if !ep.closed {
		t.Fatal("endpoint not closed")
	}
	if len(fake.unwatched) != 1 || fake.unwatched[0].ID != "role_pid" {
		t.Fatalf("expected Unwatch(role_pid), got %+v", fake.unwatched)
	}
	if s.state != StateDisconnected {
		t.Fatalf("expected StateDisconnected, got %v", s.state)
	}
	if SessionMgr().Count() != 0 {
		t.Fatalf("expected session removed from mgr, got %d", SessionMgr().Count())
	}
}

func TestSession_Terminate_WithoutRole(t *testing.T) {
	s, fake, ep := newTestSession(t)
	s.Terminate(fake, nil)
	if !ep.closed {
		t.Fatal("endpoint not closed")
	}
	if len(fake.unwatched) != 0 {
		t.Fatalf("unwatched should be empty without RolePid, got %+v", fake.unwatched)
	}
	if s.state != StateDisconnected {
		t.Fatalf("expected StateDisconnected, got %v", s.state)
	}
}

// ========== 活跃时间戳 ==========

func TestSession_UpdateLastActive(t *testing.T) {
	s, _, _ := newTestSession(t)
	old := time.Now().Add(-time.Hour)
	s.sessionInfo.ClientLastActive = old
	s.sessionInfo.ServerLastActive = old
	s.updateClientLastActive()
	s.updateServerLastActive()
	info := s.GetSessionInfo()
	if info.ClientLastActive.Before(time.Now().Add(-time.Minute)) {
		t.Fatal("ClientLastActive not updated")
	}
	if info.ServerLastActive.Before(time.Now().Add(-time.Minute)) {
		t.Fatal("ServerLastActive not updated")
	}
}
