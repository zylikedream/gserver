package logic

// Session 会话行为测试:同包白盒 + fakeActx + fakeEndpoint,
// 覆盖握手、客户端/服务端消息路由、空闲检测、断连与终止流程。

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxynet/message"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"
	"gserver/src/lib/gatetoken"

	"github.com/asynkron/protoactor-go/actor"
	"github.com/gogf/gf/v2/errors/gerror"
	"google.golang.org/protobuf/types/known/anypb"
)

// TestMain 初始化全局 actor app(不建 system)+ SessionMgr,
// 使 CallSync/LocalSend 走 "node not initialized" 错误路径而非 nil panic。
func TestMain(m *testing.M) {
	gxyactor.NewActorApp("test", "test", "127.0.0.1")
	NewSessionMgr()
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

// fakeActx 最小 actor.Context:嵌入 nil 接口兜底,只实现被测路径用到的方法。
type fakeActx struct {
	actor.Context
	self      *actor.PID
	sender    *actor.PID
	stopPID   *actor.PID
	msg       any
	watched   []*actor.PID
	unwatched []*actor.PID
}

func (f *fakeActx) Self() *actor.PID   { return f.self }
func (f *fakeActx) Sender() *actor.PID { return f.sender }
func (f *fakeActx) Stop(pid *actor.PID) {
	f.stopPID = pid
}
func (f *fakeActx) Message() any { return f.msg }
func (f *fakeActx) MessageHeader() actor.ReadonlyMessageHeader {
	return nil
}
func (f *fakeActx) Watch(pid *actor.PID)   { f.watched = append(f.watched, pid) }
func (f *fakeActx) Unwatch(pid *actor.PID) { f.unwatched = append(f.unwatched, pid) }

// unknownMsg 触发 ActorBase default 分支的 initSpan(设置 span 避免 SetName nil panic)。
type unknownMsg struct{}

// newTestSession 构造被测会话:
//   - Receive(Started): 初始化 timer/self, Init 成功(state=Connected)
//   - Receive(unknownMsg): 触发 initSpan 建立 a.span(handleHandshake/ServerMessage 需要)
func newTestSession(t *testing.T) (*Session, *fakeActx, *fakeEndpoint) {
	t.Helper()
	ep := newFakeEndpoint(t)
	s := NewSession(ep)
	fake := &fakeActx{
		self:   &actor.PID{Id: "test_session"},
		sender: &actor.PID{Id: "sender"},
		msg:    &actor.Started{},
	}
	s.Receive(fake)          // Started → Init(state=Connected, 时间初始化)
	fake.msg = &unknownMsg{} // default 分支 → initSpan
	s.Receive(fake)
	fake.stopPID = nil
	fake.watched = nil
	return s, fake, ep
}

// withHandshake 完成一次成功握手:替换 token 验证与角色激活, 返回可恢复函数。
func withHandshake(t *testing.T, s *Session, ep *fakeEndpoint) func() {
	t.Helper()
	restoreToken := swapGateTokenVerifier(func(token string) (*gatetoken.Claims, error) {
		return &gatetoken.Claims{AccountID: "acc_1", RoleID: 10001}, nil
	})
	oldActivate := activateRole
	activateRole = func(ctx context.Context, roleID int64) (gxyactor.PID, error) {
		return &actor.PID{Id: "role_pid"}, nil
	}
	oldMaint := gateMaintenanceEnabled
	gateMaintenanceEnabled = func() bool { return false }
	err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: "ok"})
	if err != nil {
		restoreToken()
		activateRole = oldActivate
		gateMaintenanceEnabled = oldMaint
		t.Fatalf("handshake: %v", err)
	}
	return func() {
		restoreToken()
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
	s, _, _ := newTestSession(t)
	if err := s.DelayInit(context.Background()); err != nil {
		t.Fatalf("DelayInit: %v", err)
	}
	info := s.GetSessionInfo()
	if info.ServerLastActive.IsZero() {
		t.Fatal("ServerLastActive not updated by DelayInit")
	}
}

// ========== HandleMessage 路由 ==========

func TestSession_HandleMessage_ClientMsg(t *testing.T) {
	s, _, ep := newTestSession(t)
	restore := withHandshake(t, s, ep)
	defer restore()
	ep.sentMsgs = nil

	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: &pb.RspAccountLogin{}}
	if err := s.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
}

func TestSession_HandleMessage_ServerMsg(t *testing.T) {
	s, _, ep := newTestSession(t)
	restore := withHandshake(t, s, ep)
	defer restore()
	ep.sentMsgs = nil

	anyMsg, err := anypb.New(&pb.RspAccountLogin{})
	if err != nil {
		t.Fatalf("anypb.New: %v", err)
	}
	serverMsg := &pb.ServerMsg{Msg: anyMsg}
	if err := s.HandleMessage(context.Background(), serverMsg); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if s.state != StateLogin {
		t.Fatalf("expected StateLogin after RspAccountLogin, got %v", s.state)
	}
}

func TestSession_HandleMessage_RoleTerminated(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, ep)
	defer restore()

	_ = s.HandleMessage(context.Background(), &actor.Terminated{Who: &actor.PID{Id: "role_pid"}})
	if fake.stopPID == nil {
		t.Fatal("expected Stop after role terminated")
	}
	if s.sessionInfo.RolePid != nil {
		t.Fatal("RolePid should be cleared")
	}
}

func TestSession_HandleMessage_RoleTerminated_OtherPid(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, ep)
	defer restore()

	_ = s.HandleMessage(context.Background(), &actor.Terminated{Who: &actor.PID{Id: "other"}})
	if fake.stopPID != nil {
		t.Fatal("unrelated Terminated should not stop session")
	}
	if s.sessionInfo.RolePid == nil {
		t.Fatal("RolePid should remain")
	}
}

func TestSession_HandleMessage_ActorError(t *testing.T) {
	s, fake, _ := newTestSession(t)
	_ = s.HandleMessage(context.Background(), &pb.ActorError{Reason: "boom"})
	if fake.stopPID == nil {
		t.Fatal("expected Stop after ActorError")
	}
}

// ========== 握手 ==========

func TestSession_Handshake_NotHandshakeMsg(t *testing.T) {
	s, _, _ := newTestSession(t)
	if err := s.handleHandshake(context.Background(), &pb.ReqChannelSend{}); err == nil {
		t.Fatal("expected error for non-handshake msg")
	}
}

func TestSession_Handshake_Maintenance(t *testing.T) {
	s, _, _ := newTestSession(t)
	old := gateMaintenanceEnabled
	gateMaintenanceEnabled = func() bool { return true }
	defer func() { gateMaintenanceEnabled = old }()

	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: "x"}); err == nil {
		t.Fatal("expected maintenance error")
	}
}

func TestSession_Handshake_EmptyToken(t *testing.T) {
	s, _, _ := newTestSession(t)
	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: ""}); err == nil {
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
		return nil, gerror.New("activate failed")
	}
	defer func() { activateRole = old }()

	if err := s.handleHandshake(context.Background(), &pb.ReqHandShake{GateToken: "ok"}); err == nil {
		t.Fatal("expected activate error")
	}
}

func TestSession_Handshake_Success(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, ep)
	defer restore()

	if s.state != StateHandshake {
		t.Fatalf("expected StateHandshake, got %v", s.state)
	}
	info := s.GetSessionInfo()
	if info.AccountID != "acc_1" || info.RoleID != 10001 {
		t.Fatalf("unexpected session info: %+v", info)
	}
	if len(fake.watched) != 1 || fake.watched[0].Id != "role_pid" {
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

// ========== 客户端消息 ==========

func TestSession_ClientMessage_Logout(t *testing.T) {
	s, fake, _ := newTestSession(t)
	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: &pb.ReqAccountLogout{}}
	if err := s.OnHandleClientMessage(context.Background(), msg); err != nil {
		t.Fatalf("OnHandleClientMessage: %v", err)
	}
	if fake.stopPID == nil {
		t.Fatal("expected Stop after client logout")
	}
}

func TestSession_ClientMessage_NotProto(t *testing.T) {
	s, _, _ := newTestSession(t)
	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: "not a proto"}
	if err := s.OnHandleClientMessage(context.Background(), msg); err == nil {
		t.Fatal("expected error for non-proto msg")
	}
}

func TestSession_ClientMessage_DataPacket_NoRolePid(t *testing.T) {
	s, _, _ := newTestSession(t) // 未握手, RolePid nil
	msg := &message.Message{Type: message.MESSAGE_TYPE_DATA_PACKET, Msg: &pb.RspAccountLogin{}}
	// SendRoleMsg → CallSync(system nil) 返回错误被忽略, 应返回 nil
	if err := s.OnHandleClientMessage(context.Background(), msg); err != nil {
		t.Fatalf("OnHandleClientMessage: %v", err)
	}
}

// ========== 服务端消息 ==========

func TestSession_ServerMessage_BadAny(t *testing.T) {
	s, _, _ := newTestSession(t)
	bad := &anypb.Any{TypeUrl: "garbage", Value: []byte{0xff}}
	if err := s.OnHandleServerMessage(context.Background(), &pb.ServerMsg{Msg: bad}); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestSession_ServerMessage_DisconnectedSkipsSend(t *testing.T) {
	s, _, ep := newTestSession(t)
	restore := withHandshake(t, s, ep)
	defer restore()
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
	s, _, ep := newTestSession(t)
	if err := s.sendClientMsg(context.Background(), &pb.RspHandShake{}); err != nil {
		t.Fatalf("sendClientMsg: %v", err)
	}
	if len(ep.sentMsgs) != 1 {
		t.Fatalf("expected 1 sent msg, got %d", len(ep.sentMsgs))
	}
}

func TestSession_SendClientMsg_FailureStopsSession(t *testing.T) {
	s, fake, ep := newTestSession(t)
	ep.sendErr = gerror.New("conn broken")
	if err := s.sendClientMsg(context.Background(), &pb.RspHandShake{}); err != nil {
		t.Fatalf("sendClientMsg should swallow send error, got %v", err)
	}
	if fake.stopPID == nil {
		t.Fatal("expected Stop after send failure")
	}
}

// ========== 空闲检测 ==========

func TestSession_SessionCheck_ClientIdle(t *testing.T) {
	s, fake, _ := newTestSession(t)
	s.sessionInfo.ClientLastActive = time.Now().Add(-SESSION_CLIENT_IDLE_TIMEOUT - time.Minute)
	s.sessionCheck(context.Background(), gxytimer.TimerActiveInfo{})
	if fake.stopPID == nil {
		t.Fatal("expected Stop after client idle")
	}
}

func TestSession_SessionCheck_ServerIdle(t *testing.T) {
	s, fake, _ := newTestSession(t)
	s.sessionInfo.ServerLastActive = time.Now().Add(-SESSION_SERVER_IDLE_TIMEOUT - time.Minute)
	s.sessionCheck(context.Background(), gxytimer.TimerActiveInfo{})
	if fake.stopPID == nil {
		t.Fatal("expected Stop after server idle")
	}
}

func TestSession_SessionCheck_Active(t *testing.T) {
	s, fake, _ := newTestSession(t)
	s.updateClientLastActive()
	s.updateServerLastActive()
	s.sessionCheck(context.Background(), gxytimer.TimerActiveInfo{})
	if fake.stopPID != nil {
		t.Fatal("active session must not stop")
	}
}

// ========== Terminate ==========

func TestSession_Terminate_WithRole(t *testing.T) {
	s, fake, ep := newTestSession(t)
	restore := withHandshake(t, s, ep)
	defer restore()

	s.Terminate(context.Background(), gerror.New("client account logout"))
	if !ep.closed {
		t.Fatal("endpoint not closed")
	}
	if len(fake.unwatched) != 1 || fake.unwatched[0].Id != "role_pid" {
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
	s.Terminate(context.Background(), nil)
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
