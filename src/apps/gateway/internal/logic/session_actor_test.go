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
	"gserver/core/gxyactor/gxyactortest"
	"gserver/core/gxymetrics"
	"gserver/core/gxynet/message"
	"gserver/protocol/pb"
	"gserver/src/lib/gatetoken"

	"ergo.services/ergo/gen"
	"ergo.services/ergo/testing/unit"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/protobuf/types/known/anypb"
)

// TestMain 初始化全局 actor app(不建 system),
// 使 CallSync/LocalSend 走 "node not initialized" 错误路径而非 nil panic。
func TestMain(m *testing.M) {
	gxyactor.NewActorApp("test", "test", "127.0.0.1")
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

// roleRuntimePID 是握手时激活得到的角色进程标识。
// 监视通知携带运行时标识,测试用同一个值构造 Down 消息。
var roleRuntimePID = gen.PID{Node: "test@127.0.0.1", ID: 42, Creation: 1}

// newTestDeps 是测试用依赖:允许登录、返回固定身份与固定的激活结果。
// 每个测试拿自己的一份,会话注册表因此互不可见。
func newTestDeps() SessionDeps {
	return SessionDeps{
		VerifyToken: func(string) (*gatetoken.Claims, error) {
			return &gatetoken.Claims{AccountID: "acc_1", RoleID: 10001}, nil
		},
		Login: &stubLoginAcquirer{permit: noopLoginPermit{}},
		ActivateRole: func(context.Context, int64) (gxyactor.PID, error) {
			return gxyactor.PidFromRuntime(roleRuntimePID), nil
		},
		Sessions: gxyactor.NewActorMgr("session_mgr_test"),
	}
}

// newTestSession 在 mock 节点上创建真实 actor,而不是手工拼装半成品。
// 返回 Subject 以便断言运行时的出向记录(如 Monitor)。
func newTestSession(t *testing.T, d SessionDeps) (*Session, *unit.Subject, *fakeEndpoint) {
	t.Helper()
	ep := newFakeEndpoint(t)
	s, subj := gxyactortest.Spawn(t, "session", func() *Session { return NewSession(ep, d) })
	return s, subj, ep
}

// withHandshake 完成一次成功握手。会话依赖由 newTestDeps 在创建时给定。
func withHandshake(t *testing.T, s *Session) {
	t.Helper()
	// 空串使维护闸门关闭,不依赖测试进程的环境。
	t.Setenv(gateMaintenanceEnv, "")
	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: "ok"}); err != nil {
		t.Fatalf("handshake: %v", err)
	}
}

// ========== sessionDisconnectReason ==========

func TestSessionDisconnectReason(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		// 无原因即正常结束(连接干净关闭):归到 conn_closed,不是 unknown。
		{nil, "conn_closed"},
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

// ========== Init ==========

func TestSession_Init(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	if s.state != StateConnected {
		t.Fatalf("expected StateConnected, got %v", s.state)
	}
	info := s.GetSessionInfo()
	if info.ConnectTime.IsZero() || info.ClientLastActive.IsZero() {
		t.Fatalf("session times not initialized: %+v", info)
	}
}

// Init 应记录最后活跃时间(空闲检查的依据)。
func TestSession_Init_RecordsLastActive(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	info := s.GetSessionInfo()
	if info.ServerLastActive.IsZero() {
		t.Fatal("ServerLastActive not updated by Init")
	}
}

// ========== HandleMessage 路由 ==========

func TestSession_HandleMessage_ClientMsg(t *testing.T) {
	s, _, ep := newTestSession(t, newTestDeps())
	withHandshake(t, s)
	ep.sentMsgs = nil

	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: &pb.RspAccountLogin{}}
	if _, err := s.HandleMessage(msg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
}

func TestSession_HandleMessage_ServerMsg(t *testing.T) {
	s, _, ep := newTestSession(t, newTestDeps())
	withHandshake(t, s)
	ep.sentMsgs = nil

	anyMsg, err := anypb.New(&pb.RspAccountLogin{})
	if err != nil {
		t.Fatalf("anypb.New: %v", err)
	}
	serverMsg := &pb.ServerMsg{Msg: anyMsg}
	if _, err := s.HandleMessage(serverMsg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if s.state != StateLogin {
		t.Fatalf("expected StateLogin after RspAccountLogin, got %v", s.state)
	}
}

// 被监视的角色终止时,会话必须自行停止并清空角色引用。
//
// 监视通知由运行时投递,门面把它翻译成业务侧的 HandleDown——业务因此
// 不出现运行时消息类型(见 ADR 0017)。此处经真实投递验证这条翻译。
func TestSession_HandleDown_RoleTerminated(t *testing.T) {
	s, subj, _ := newTestSession(t, newTestDeps())
	withHandshake(t, s)

	subj.SendMessage(gen.PID{}, gen.MessageDownPID{PID: roleRuntimePID})

	if !s.StopRequested() {
		t.Fatal("expected Stop after role terminated")
	}
	if !gxyactor.PIDIsZero(s.sessionInfo.RolePid) {
		t.Fatal("RolePid should be cleared")
	}
}

func TestSession_HandleMessage_RoleTerminated_OtherPid(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	withHandshake(t, s)

	_, _ = s.HandleMessage(gen.MessageDownPID{PID: gen.PID{Node: "test@127.0.0.1", ID: 99, Creation: 1}})
	if s.StopRequested() {
		t.Fatal("unrelated Terminated should not stop session")
	}
	if gxyactor.PIDIsZero(s.sessionInfo.RolePid) {
		t.Fatal("RolePid should remain")
	}
}

func TestSession_HandleMessage_ActorError(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	_, _ = s.HandleMessage(&pb.ActorError{Reason: "boom"})
	if !s.StopRequested() {
		t.Fatal("expected Stop after ActorError")
	}
}

// ========== 握手 ==========

func TestSession_Handshake_NotHandshakeMsg(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	if err := s.handleHandshake(context.Background(), &pb.ReqChannelSend{}); err == nil {
		t.Fatal("expected error for non-handshake msg")
	}
}

func TestSession_Handshake_Maintenance(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	t.Setenv(gateMaintenanceEnv, "1")

	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: "x"}); err == nil {
		t.Fatal("expected maintenance error")
	}
}

func TestSession_Handshake_EmptyToken(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: ""}); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestSession_Handshake_ActivateRoleFailed(t *testing.T) {
	d := newTestDeps()
	d.ActivateRole = func(context.Context, int64) (gxyactor.PID, error) {
		return gxyactor.PID{}, gerror.New("activate failed")
	}
	s, _, _ := newTestSession(t, d)
	t.Setenv(gateMaintenanceEnv, "")

	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: "ok"}); err == nil {
		t.Fatal("expected activate error")
	}
}

func TestSession_Handshake_Success(t *testing.T) {
	d := newTestDeps()
	s, subj, ep := newTestSession(t, d)
	withHandshake(t, s)

	if s.state != StateHandshake {
		t.Fatalf("expected StateHandshake, got %v", s.state)
	}
	info := s.GetSessionInfo()
	if info.AccountID != "acc_1" || info.RoleID != 10001 {
		t.Fatalf("unexpected session info: %+v", info)
	}
	subj.ShouldMonitor().From(subj.PID()).Target(roleRuntimePID).Once().Assert()
	if len(ep.sentMsgs) != 1 {
		t.Fatalf("expected 1 handshake rsp, got %d", len(ep.sentMsgs))
	}
	if _, ok := ep.sentMsgs[0].(*pb.RspHandShake); !ok {
		t.Fatalf("expected RspHandShake, got %T", ep.sentMsgs[0])
	}
	if d.Sessions.Count() != 1 {
		t.Fatalf("expected 1 session in mgr, got %d", d.Sessions.Count())
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
			d := newTestDeps()
			d.Login = &stubLoginAcquirer{err: tc.err}
			d.ActivateRole = func(context.Context, int64) (gxyactor.PID, error) {
				t.Fatal("ActivateRole must not be called on admission rejection")
				return gxyactor.PID{}, nil
			}
			s, _, ep := newTestSession(t, d)
			before := testutil.ToFloat64(gxymetrics.SessionDisconnects.WithLabelValues(tc.label))
			t.Setenv(gateMaintenanceEnv, "")

			msg := &message.Message{
				Type: message.MESSGE_TYPE_FIRST_PACKET,
				Msg:  &pb.ReqHandShake{GateToken: "ok"},
			}
			if err := s.OnHandleClientMessage(context.Background(), msg, gxyactor.PID{}); err != nil {
				t.Fatalf("expected nil at actor boundary, got: %v", err)
			}
			if !s.StopRequested() {
				t.Fatal("expected Stop after admission rejection")
			}
			if s.state != StateConnected {
				t.Fatalf("session state = %v, want StateConnected", s.state)
			}
			if !gxyactor.PIDIsZero(s.sessionInfo.RolePid) {
				t.Fatal("RolePid must stay nil after admission rejection")
			}
			// 端到端:运行时以 handler 返回的终止原因调用 Terminate, 它据此计算
			// 断连标签; sentinel 错误对象必须原样到达, 不因包裹/重建而降级。
			s.Terminate(tc.err)
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
	permit := &recordingLoginPermit{}
	var heldDuringActivate bool
	d := newTestDeps()
	d.Login = &stubLoginAcquirer{permit: permit}
	d.ActivateRole = func(context.Context, int64) (gxyactor.PID, error) {
		heldDuringActivate = permit.releases == 0
		return gxyactor.PidFromRuntime(roleRuntimePID), nil
	}
	s, subj, ep := newTestSession(t, d)
	t.Setenv(gateMaintenanceEnv, "")

	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: "ok"}); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if !heldDuringActivate {
		t.Fatal("permit was not held while ActivateRole ran")
	}
	if permit.releases != 1 {
		t.Fatalf("permit released %d times, want exactly 1", permit.releases)
	}
	subj.ShouldMonitor().From(subj.PID()).Target(roleRuntimePID).Once().Assert()
	if len(ep.sentMsgs) != 1 {
		t.Fatalf("expected 1 handshake rsp after release, got %d", len(ep.sentMsgs))
	}
}

// TestSession_LoginAdmission_ActivateRoleErrorReleases activateRole 失败时仍恰好释放一次。
func TestSession_LoginAdmission_ActivateRoleErrorReleases(t *testing.T) {
	permit := &recordingLoginPermit{}
	d := newTestDeps()
	d.Login = &stubLoginAcquirer{permit: permit}
	d.ActivateRole = func(context.Context, int64) (gxyactor.PID, error) {
		return gxyactor.PID{}, gerror.New("activate failed")
	}
	s, _, _ := newTestSession(t, d)
	t.Setenv(gateMaintenanceEnv, "")

	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: "ok"}); err == nil {
		t.Fatal("expected ActivateRole error")
	}
	if permit.releases != 1 {
		t.Fatalf("permit released %d times, want exactly 1", permit.releases)
	}
}

// TestSession_LoginAdmission_UnconfiguredPropagates 未配置限流器属于启动/接线缺陷,
// 不应被归类为预期拒绝: 错误必须传播到 Actor 边界, 且不停止 Session。
func TestSession_LoginAdmission_UnconfiguredPropagates(t *testing.T) {
	d := SessionDeps{
		VerifyToken: newTestDeps().VerifyToken,
		ActivateRole: func(context.Context, int64) (gxyactor.PID, error) {
			t.Fatal("ActivateRole must not be called when limiter is unconfigured")
			return gxyactor.PID{}, nil
		},
		Sessions: gxyactor.NewActorMgr("session_mgr_test"),
	}
	s, _, _ := newTestSession(t, d)
	t.Setenv(gateMaintenanceEnv, "")

	msg := &message.Message{
		Type: message.MESSGE_TYPE_FIRST_PACKET,
		Msg:  &pb.ReqHandShake{GateToken: "ok"},
	}
	err := s.OnHandleClientMessage(context.Background(), msg, gxyactor.PID{})
	if err == nil {
		t.Fatal("expected unconfigured error to propagate to actor boundary")
	}
	if isLoginAdmissionRejection(err) {
		t.Fatalf("unconfigured must not be classified as an expected rejection, got: %v", err)
	}
	if !strings.Contains(err.Error(), "login limiter not configured") {
		t.Fatalf("propagated error = %v, want ErrLoginLimiterUnconfigured", err)
	}
	if s.StopRequested() {
		t.Fatal("unconfigured wiring defect must not stop the session")
	}
}

// ========== 客户端消息 ==========

func TestSession_ClientMessage_Logout(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: &pb.ReqAccountLogout{}}
	if err := s.OnHandleClientMessage(context.Background(), msg, gxyactor.PID{}); err != nil {
		t.Fatalf("OnHandleClientMessage: %v", err)
	}
	if !s.StopRequested() {
		t.Fatal("expected Stop after client logout")
	}
}

func TestSession_ClientMessage_NotProto(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: "not a proto"}
	if err := s.OnHandleClientMessage(context.Background(), msg, gxyactor.PID{}); err == nil {
		t.Fatal("expected error for non-proto msg")
	}
}

func TestSession_ClientMessage_DataPacket_NoRolePid(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps()) // 未握手, RolePid nil
	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: &pb.RspAccountLogin{}}
	// SendRoleMsg 的投递错误只记日志,不使消息处理失败,应返回 nil
	if err := s.OnHandleClientMessage(context.Background(), msg, gxyactor.PID{}); err != nil {
		t.Fatalf("OnHandleClientMessage: %v", err)
	}
}

// ========== 服务端消息 ==========

func TestSession_ServerMessage_BadAny(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	bad := &anypb.Any{TypeUrl: "garbage", Value: []byte{0xff}}
	if err := s.OnHandleServerMessage(context.Background(), &pb.ServerMsg{Msg: bad}); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestSession_ServerMessage_DisconnectedSkipsSend(t *testing.T) {
	s, _, ep := newTestSession(t, newTestDeps())
	withHandshake(t, s)
	ep.sentMsgs = nil
	s.state = StateDisconnected

	// RspAccountLogin 会重置 state=Login, 用普通响应验证 Disconnected 跳过
	anyMsg, _ := anypb.New(&pb.RspHandShake{})
	if err := s.OnHandleServerMessage(context.Background(), &pb.ServerMsg{Msg: anyMsg}); err != nil {
		t.Fatalf("OnHandleServerMessage: %v", err)
	}
	if len(ep.sentMsgs) != 0 {
		t.Fatalf("disconnected session must not send, got %d msgs", len(ep.sentMsgs))
	}
}

// ========== sendClientMsg ==========

func TestSession_SendClientMsg_Success(t *testing.T) {
	s, _, ep := newTestSession(t, newTestDeps())
	if err := s.sendClientMsg(context.Background(), &pb.RspHandShake{}); err != nil {
		t.Fatalf("sendClientMsg: %v", err)
	}
	if len(ep.sentMsgs) != 1 {
		t.Fatalf("expected 1 sent msg, got %d", len(ep.sentMsgs))
	}
}

func TestSession_SendClientMsg_FailureStopsSession(t *testing.T) {
	s, _, ep := newTestSession(t, newTestDeps())
	ep.sendErr = gerror.New("conn broken")
	if err := s.sendClientMsg(context.Background(), &pb.RspHandShake{}); err != nil {
		t.Fatalf("sendClientMsg should swallow send error, got %v", err)
	}
	if !s.StopRequested() {
		t.Fatal("expected Stop after send failure")
	}
}

// ========== 空闲检测 ==========

func TestSession_SessionCheck_ClientIdle(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	s.sessionInfo.ClientLastActive = time.Now().Add(-SESSION_CLIENT_IDLE_TIMEOUT - time.Minute)
	s.sessionCheck(context.Background())
	if !s.StopRequested() {
		t.Fatal("expected Stop after client idle")
	}
}

func TestSession_SessionCheck_ServerIdle(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	s.sessionInfo.ServerLastActive = time.Now().Add(-SESSION_SERVER_IDLE_TIMEOUT - time.Minute)
	s.sessionCheck(context.Background())
	if !s.StopRequested() {
		t.Fatal("expected Stop after server idle")
	}
}

func TestSession_SessionCheck_Active(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
	s.updateClientLastActive()
	s.updateServerLastActive()
	s.sessionCheck(context.Background())
	if s.StopRequested() {
		t.Fatal("active session must not stop")
	}
}

// ========== Terminate ==========

func TestSession_Terminate_WithRole(t *testing.T) {
	d := newTestDeps()
	s, _, ep := newTestSession(t, d)
	withHandshake(t, s)

	s.Terminate(gerror.New("client account logout"))
	if !ep.closed {
		t.Fatal("endpoint not closed")
	}
	if s.state != StateDisconnected {
		t.Fatalf("expected StateDisconnected, got %v", s.state)
	}
	if d.Sessions.Count() != 0 {
		t.Fatalf("expected session removed from mgr, got %d", d.Sessions.Count())
	}
}

// 正常结束(无原因)带角色时也必须走完终止路径:早先此处无条件取 err.Error(),
// 而"客户端干净断开"正是 err=nil 的量级最大的一条路径。
func TestSession_Terminate_WithRole_NilReason(t *testing.T) {
	d := newTestDeps()
	s, _, ep := newTestSession(t, d)
	withHandshake(t, s)

	s.Terminate(nil)
	if !ep.closed {
		t.Fatal("endpoint not closed")
	}
	if s.state != StateDisconnected {
		t.Fatalf("expected StateDisconnected, got %v", s.state)
	}
	if d.Sessions.Count() != 0 {
		t.Fatalf("expected session removed from mgr, got %d", d.Sessions.Count())
	}
}

func TestSession_Terminate_WithoutRole(t *testing.T) {
	s, _, ep := newTestSession(t, newTestDeps())
	s.Terminate(nil)
	if !ep.closed {
		t.Fatal("endpoint not closed")
	}
	if s.state != StateDisconnected {
		t.Fatalf("expected StateDisconnected, got %v", s.state)
	}
}

// ========== 活跃时间戳 ==========

func TestSession_UpdateLastActive(t *testing.T) {
	s, _, _ := newTestSession(t, newTestDeps())
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
