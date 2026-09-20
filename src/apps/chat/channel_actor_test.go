package chat

// ChannelActor 行为测试:同包白盒,用 mock 节点创建真实 actor,
// 覆盖 HandleMessage 各消息分支、save 持久化与 actor 生命周期。

import (
	"context"
	"gserver/src/lib"
	"os"
	"testing"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxyactor/gxyactortest"
	"gserver/protocol/pb"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestMain 初始化全局 actor app(不建 system/不绑端口),
// 使 PublishRoleNotify/Respond 走 "node not initialized" 错误路径而非 nil panic。
func TestMain(m *testing.M) {
	gxyactor.NewActorApp("test", "test", "127.0.0.1")
	os.Exit(m.Run())
}

// newTestChannelActor 在 mock 节点上创建真实 actor,并注入 channel/buffer。
// 用运行时创建一个已绑定的真实实例,而不是手工拼装半成品。
func newTestChannelActor(t *testing.T, ch IChannel) *ChannelActor {
	t.Helper()
	gxyactortest.StubOwnership(t)
	a, _ := gxyactortest.Spawn(t, lib.CHANNEL_ACTOR_TYPE, NewChannelActor, "1_100")
	a.channel = ch
	a.buffer = newRingBuffer(ch.RingBufferSize())
	return a
}

// newGormDB 用 sqlmock 构造 gorm 连接。
func newGormDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	return db, mock
}

// expectChannelInsert 断言一次 chat_guild_message INSERT:
// gorm 默认事务 + Create(map 无主键)走 Exec(无 RETURNING)。
func expectChannelInsert(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO "`+chatGuildMessageTable+`"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
}

// ========== Init ==========

func TestChannelActor_Init_Valid(t *testing.T) {
	a := newTestChannelActor(t, GuildChannel{})
	if a.ChannelType != 1 || a.ChannelID != 100 {
		t.Fatalf("expected type=1 id=100, got type=%d id=%d", a.ChannelType, a.ChannelID)
	}
	if a.channel == nil {
		t.Fatal("channel not resolved")
	}
	if a.buffer == nil || a.buffer.Len() != 0 {
		t.Fatalf("buffer not initialized: %+v", a.buffer)
	}
}

func TestChannelActor_Init_NoArgs(t *testing.T) {
	a := NewChannelActor()
	if err := a.Init(); err == nil {
		t.Fatal("expected error for missing args")
	}
}

func TestChannelActor_Init_InvalidFormat(t *testing.T) {
	a := NewChannelActor()
	if err := a.Init("abc"); err == nil {
		t.Fatal("expected error for invalid id format")
	}
}

func TestChannelActor_Init_UnknownChannelType(t *testing.T) {
	a := NewChannelActor()
	if err := a.Init("99_1"); err == nil {
		t.Fatal("expected error for unknown channel type")
	}
}

// ========== 成员注册/注销 ==========

func TestChannelActor_Register_AddsMember(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	_, err := a.HandleMessage(&pb.ChannelRegisterMsg{
		RoleId: 5,
		Pid:    &pb.ActorPid{Address: "addr1", Id: "pid5"},
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	m, ok := a.members[5]
	if !ok {
		t.Fatal("member 5 not registered")
	}
	if m.RoleID != 5 {
		t.Fatalf("unexpected member: %+v", m)
	}
	if m.JoinTime.IsZero() {
		t.Fatal("JoinTime not set")
	}
}

func TestChannelActor_Register_OverwriteExisting(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	msg := &pb.ChannelRegisterMsg{RoleId: 5, Pid: &pb.ActorPid{Id: "pid_old"}}
	if _, err := a.HandleMessage(msg); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if _, err := a.HandleMessage(msg); err != nil {
		t.Fatalf("second register: %v", err)
	}
	if len(a.members) != 1 {
		t.Fatalf("re-register must not add a second entry, got %d", len(a.members))
	}
	if a.members[5].RoleID != 5 {
		t.Fatalf("expected member 5 to remain, got %+v", a.members[5])
	}
}

func TestChannelActor_Unregister_RemovesMember(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	reg := func(id int64) {
		t.Helper()
		if _, err := a.HandleMessage(&pb.ChannelRegisterMsg{
			RoleId: id, Pid: &pb.ActorPid{Id: "p" + string(rune(id))},
		}); err != nil {
			t.Fatalf("register %d: %v", id, err)
		}
	}
	reg(5)
	reg(6)
	if _, err := a.HandleMessage(&pb.ChannelUnregisterMsg{RoleId: 5}); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if len(a.members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(a.members))
	}
	if _, ok := a.members[6]; !ok {
		t.Fatal("member 6 should remain")
	}
}

func TestChannelActor_Unregister_LastMemberNoPanic(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	if _, err := a.HandleMessage(&pb.ChannelRegisterMsg{
		RoleId: 5, Pid: &pb.ActorPid{Id: "pid5"},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := a.HandleMessage(&pb.ChannelUnregisterMsg{RoleId: 5}); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if len(a.members) != 0 {
		t.Fatalf("expected 0 members, got %d", len(a.members))
	}
}

// ========== 消息发送 ==========

func TestChannelActor_Send_EmptyContentRejected(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	_, err := a.HandleMessage(&pb.ReqChannelSend{
		ChannelType: 1, ChannelId: 100, SenderId: 5, Content: "",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if a.buffer.Len() != 0 {
		t.Fatalf("empty content must not be buffered, got %d", a.buffer.Len())
	}
}

func TestChannelActor_Send_AppendsToBuffer(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	_, err := a.HandleMessage(&pb.ReqChannelSend{
		ChannelType: 1, ChannelId: 100, SenderId: 5, Content: "hello",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if a.buffer.Len() != 1 {
		t.Fatalf("expected 1 buffered msg, got %d", a.buffer.Len())
	}
	msgs := a.buffer.Recent(1)
	if msgs[0].Content != "hello" || msgs[0].Timestamp == 0 {
		t.Fatalf("unexpected msg: %+v", msgs[0])
	}
}

func TestChannelActor_Send_WithMembersNoPanic(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	// RoleID<=0: PublishRoleNotify 走 invalid 分支(不触达未初始化的全局 Redis),
	// 测试聚焦"通知所有成员"流程不 panic + buffer 追加。
	for _, id := range []int64{0, -1} {
		if _, err := a.HandleMessage(&pb.ChannelRegisterMsg{
			RoleId: id, Pid: &pb.ActorPid{Id: "p" + string(rune(id))},
		}); err != nil {
			t.Fatalf("register %d: %v", id, err)
		}
	}
	// 通知所有成员(PublishRoleNotify 经全局 app 失败无害), 不应 panic
	if _, err := a.HandleMessage(&pb.ReqChannelSend{
		ChannelType: 1, ChannelId: 100, SenderId: 5, Content: "hi",
	}); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if a.buffer.Len() != 1 {
		t.Fatalf("expected 1 buffered msg, got %d", a.buffer.Len())
	}
}

// ========== 历史记录 ==========

func TestChannelActor_History_ReturnsBuffered(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	send := func(content string) {
		t.Helper()
		if _, err := a.HandleMessage(&pb.ReqChannelSend{
			ChannelType: 1, ChannelId: 100, SenderId: 5, Content: content,
		}); err != nil {
			t.Fatalf("send %q: %v", content, err)
		}
	}
	send("m1")
	send("m2")
	send("m3")

	if _, err := a.HandleMessage(&pb.ReqChatChannelHistory{
		ChannelType: 1, ChannelId: 100, Count: 2,
	}); err != nil {
		t.Fatalf("history: %v", err)
	}
	msgs := a.buffer.Recent(2)
	if len(msgs) != 2 || msgs[0].Content != "m2" || msgs[1].Content != "m3" {
		t.Fatalf("expected last 2 msgs, got %+v", msgs)
	}
}

func TestChannelActor_History_CountClamped(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	if _, err := a.HandleMessage(&pb.ReqChannelSend{
		ChannelType: 1, ChannelId: 100, SenderId: 5, Content: "x",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	// count<=0 与超上限都应 clamp 到 RingBufferSize, 不 panic
	for _, c := range []int32{0, -1, 100000} {
		if _, err := a.HandleMessage(&pb.ReqChatChannelHistory{
			ChannelType: 1, ChannelId: 100, Count: c,
		}); err != nil {
			t.Fatalf("history count=%d: %v", c, err)
		}
	}
}

// ========== save 持久化 ==========

// TestChannelActor_Save_PersistsNewMessages GuildChannel(SaveInterval>0):
// 新消息逐条 INSERT, lastSavedSeq 前进。
func TestChannelActor_Save_PersistsNewMessages(t *testing.T) {
	db, mock := newGormDB(t)
	a := newTestChannelActor(t, GuildChannel{})
	a.db = db

	for range 2 {
		expectChannelInsert(mock)
	}
	for _, c := range []string{"a", "b"} {
		if _, err := a.HandleMessage(&pb.ReqChannelSend{
			ChannelType: 4, ChannelId: 7, SenderId: 5, Content: c,
		}); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	a.save(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("save inserts not met: %v", err)
	}
	if a.lastSavedSeq != 2 {
		t.Fatalf("expected lastSavedSeq=2, got %d", a.lastSavedSeq)
	}
}

// TestChannelActor_Save_NoNewMessagesSkipsWrite 已保存无新消息: 不写库。
func TestChannelActor_Save_NoNewMessagesSkipsWrite(t *testing.T) {
	db, mock := newGormDB(t)
	a := newTestChannelActor(t, GuildChannel{})
	a.db = db

	expectChannelInsert(mock)
	if _, err := a.HandleMessage(&pb.ReqChannelSend{
		ChannelType: 4, ChannelId: 7, SenderId: 5, Content: "x",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	a.save(context.Background())
	a.save(context.Background()) // 第二次不应产生 INSERT
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected db write: %v", err)
	}
}

// TestChannelActor_Save_DisabledChannelSkips WorldChannel(SaveInterval=0): 不写库。
func TestChannelActor_Save_DisabledChannelSkips(t *testing.T) {
	db, mock := newGormDB(t)
	a := newTestChannelActor(t, WorldChannel{})
	a.db = db
	if _, err := a.HandleMessage(&pb.ReqChannelSend{
		ChannelType: 1, ChannelId: 100, SenderId: 5, Content: "x",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	a.save(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unexpected db write: %v", err)
	}
	if a.lastSavedSeq != 0 {
		t.Fatalf("expected lastSavedSeq=0, got %d", a.lastSavedSeq)
	}
}

// ========== 生命周期 ==========

// Init 对保存间隔>0 的频道应注册周期落盘定时器。
func TestChannelActor_Init_WithSaveInterval(t *testing.T) {
	a := newTestChannelActor(t, GuildChannel{})
	if a.channel.SaveInterval() <= 0 {
		t.Fatal("test channel must have a positive save interval")
	}
}

// Init 对无保存间隔的频道不注册定时器,且不应出错。
func TestChannelActor_Init_WithoutSaveInterval(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	if a.channel.SaveInterval() != 0 {
		t.Fatal("test channel must have no save interval")
	}
}

func TestChannelActor_Terminate_NoPanic(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	a.Terminate(nil)
}

// TestChannelActor_Terminate_PersistsPending SaveInterval>0 时 Terminate 落盘。
func TestChannelActor_Terminate_PersistsPending(t *testing.T) {
	db, mock := newGormDB(t)
	a := newTestChannelActor(t, GuildChannel{})
	a.db = db
	expectChannelInsert(mock)
	if _, err := a.HandleMessage(&pb.ReqChannelSend{
		ChannelType: 4, ChannelId: 7, SenderId: 5, Content: "bye",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	a.Terminate(nil)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("terminate save not met: %v", err)
	}
}

// TestChannelActor_RingBuffer_Eviction 消息超上限滚动淘汰(容量 200)。
func TestChannelActor_RingBuffer_Eviction(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	for i := range 205 {
		if _, err := a.HandleMessage(&pb.ReqChannelSend{
			ChannelType: 1, ChannelId: 100, SenderId: 5, Content: "m",
		}); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if a.buffer.Len() != 200 {
		t.Fatalf("expected buffer len 200, got %d", a.buffer.Len())
	}
	first := a.buffer.Recent(200)[0]
	if first.Timestamp == 0 {
		t.Fatal("evicted buffer should contain valid msgs")
	}
	_ = time.Now() // 保持 time import(JoinTime 断言)
}

// ========== loadHistory 启动加载 ==========

// TestChannelActor_LoadHistory_Populates 启动从 chat_guild_message 加载最近历史:
// DESC 查询结果按正序填充 buffer, lastSavedSeq 对齐防重复落库。
func TestChannelActor_LoadHistory_Populates(t *testing.T) {
	db, mock := newGormDB(t)
	a := newTestChannelActor(t, GuildChannel{})
	a.db = db
	a.ChannelType = 4
	a.ChannelID = 7

	// DESC: 最新(9, "later")在前; buffer 应为正序: (8, "first") → (9, "later")
	mock.ExpectQuery(`SELECT .* FROM "`+chatGuildMessageTable+`"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"sender_id", "content", "timestamp"}).
			AddRow(9, "later", 200).
			AddRow(8, "first", 100))

	a.loadHistory(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("loadHistory query not met: %v", err)
	}
	if a.buffer.Len() != 2 {
		t.Fatalf("expected 2 buffered msgs, got %d", a.buffer.Len())
	}
	msgs := a.buffer.Recent(2)
	if msgs[0].Content != "first" || msgs[0].Sender.GetRoleId() != 8 ||
		msgs[1].Content != "later" || msgs[1].Sender.GetRoleId() != 9 {
		t.Fatalf("unexpected history order/content: %+v / %+v", msgs[0], msgs[1])
	}
	if a.lastSavedSeq != 2 {
		t.Fatalf("expected lastSavedSeq=2, got %d", a.lastSavedSeq)
	}
}

// TestChannelActor_LoadHistory_Empty 无历史记录: 空 buffer, 不报错。
func TestChannelActor_LoadHistory_Empty(t *testing.T) {
	db, mock := newGormDB(t)
	a := newTestChannelActor(t, GuildChannel{})
	a.db = db
	a.ChannelType = 4
	a.ChannelID = 7

	mock.ExpectQuery(`SELECT .* FROM "`+chatGuildMessageTable+`"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"sender_id", "content", "timestamp"}))

	a.loadHistory(context.Background())
	if a.buffer.Len() != 0 {
		t.Fatalf("expected empty buffer, got %d", a.buffer.Len())
	}
	if a.lastSavedSeq != 0 {
		t.Fatalf("expected lastSavedSeq=0, got %d", a.lastSavedSeq)
	}
}

// TestChannelActor_LoadHistory_Disabled 无存盘频道(World)不查库。
func TestChannelActor_LoadHistory_Disabled(t *testing.T) {
	db, mock := newGormDB(t)
	a := newTestChannelActor(t, WorldChannel{})
	a.db = db
	a.loadHistory(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("disabled channel must not query db: %v", err)
	}
}

// TestChannelActor_LoadHistory_DBError 查询失败: 记日志继续, 不 panic。
func TestChannelActor_LoadHistory_DBError(t *testing.T) {
	db, mock := newGormDB(t)
	a := newTestChannelActor(t, GuildChannel{})
	a.db = db
	a.ChannelType = 4
	a.ChannelID = 7

	mock.ExpectQuery(`SELECT .* FROM "`+chatGuildMessageTable+`"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(gorm.ErrInvalidDB)

	a.loadHistory(context.Background()) // 不 panic, buffer 保持空
	if a.buffer.Len() != 0 {
		t.Fatalf("buffer should stay empty on error, got %d", a.buffer.Len())
	}
}

// TestChannelActor_Send_PersistsSenderID 落库 sender_id 为真实发送者。
func TestChannelActor_Send_PersistsSenderID(t *testing.T) {
	db, mock := newGormDB(t)
	a := newTestChannelActor(t, GuildChannel{})
	a.db = db
	a.ChannelType = 4
	a.ChannelID = 7

	mock.ExpectBegin()
	// gorm map 列按字母序: channel_id, channel_type, content, sender_id, timestamp
	mock.ExpectExec(`INSERT INTO "`+chatGuildMessageTable+`"`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(5), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if _, err := a.HandleMessage(&pb.ReqChannelSend{
		ChannelType: 4, ChannelId: 7, SenderId: 5, Content: "hi",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	a.save(context.Background())
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sender_id not persisted as expected: %v", err)
	}
	// buffer 内消息 Sender 也应正确
	msgs := a.buffer.Recent(1)
	if msgs[0].Sender.GetRoleId() != 5 {
		t.Fatalf("expected buffered msg sender 5, got %d", msgs[0].Sender.GetRoleId())
	}
}
