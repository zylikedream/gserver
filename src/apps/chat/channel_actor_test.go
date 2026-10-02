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

// TestChannelActor_Init_Valid:入参 "1_100" 必须解析出 key{Type:1,ID:100},并留下可用的 ringBuffer。
// 为什么需要:key 是频道 actor 的寻址基准,解析偏差会让后续所有消息被路由到错误的频道 actor。
// 注意:helper 在 Init 之后把 a.channel/a.buffer 覆写为注入值,故 channel 非 nil 只反映注入,不证明 Init 解析出了 WORLD 频道。
func TestChannelActor_Init_Valid(t *testing.T) {
	a := newTestChannelActor(t, GuildChannel{})
	if a.key.Type != 1 || a.key.ID != 100 {
		t.Fatalf("expected type=1 id=100, got type=%d id=%d", a.key.Type, a.key.ID)
	}
	if a.channel == nil {
		t.Fatal("channel not resolved")
	}
	if a.buffer == nil || a.buffer.Len() != 0 {
		t.Fatalf("buffer not initialized: %+v", a.buffer)
	}
}

// TestChannelActor_Init_NoArgs:无参调用必须返回错误。
// 为什么需要:见 Init 首段注释——先占一份归属再回滚,会让一次注定失败的创建把所有权协调层搅进去。
func TestChannelActor_Init_NoArgs(t *testing.T) {
	a := NewChannelActor()
	if err := a.Init(); err == nil {
		t.Fatal("expected error for missing args")
	}
}

// TestChannelActor_Init_InvalidFormat:"abc" 解析不出 type_id,Init 必须拒绝。
// 为什么需要:放过它就会把一个 kind/id 全零的坏 actor 注册进集群,此后消息静默投递到"频道 0_0"。
func TestChannelActor_Init_InvalidFormat(t *testing.T) {
	a := NewChannelActor()
	if err := a.Init("abc"); err == nil {
		t.Fatal("expected error for invalid id format")
	}
}

// TestChannelActor_Init_UnknownChannelType:"99_1" 解析成功但类型不在 channelRegistry,Init 必须报错。
// 为什么需要:GetChannel 返回 !ok 时若继续执行,buffer 会挂在一个不存在的频道上,首次发消息就在 CanWrite 上空指针。
func TestChannelActor_Init_UnknownChannelType(t *testing.T) {
	a := NewChannelActor()
	if err := a.Init("99_1"); err == nil {
		t.Fatal("expected error for unknown channel type")
	}
}

// ========== 成员注册/注销 ==========

// TestChannelActor_Register_AddsMember:注册消息把 RoleID 记入 members,并盖上非零 JoinTime。
// 为什么需要:members 就是 handleChannelSend 的广播名单,漏记则该玩家永远收不到频道推送,且没有任何报错。
func TestChannelActor_Register_AddsMember(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	_, err := a.HandleMessage(&pb.ChannelRegisterMsg{
		RoleId: 5,
		Pid:    &pb.ActorPid{Address: "addr1", Name: "pid5"},
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

// TestChannelActor_Register_OverwriteExisting:同一 RoleID 重复注册必须幂等,members 仍只有 1 条。
// 为什么需要:断线重连会让同一玩家注册两次;不幂等则广播名单翻倍,同一条消息被推送两遍。
func TestChannelActor_Register_OverwriteExisting(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	msg := &pb.ChannelRegisterMsg{RoleId: 5, Pid: &pb.ActorPid{Name: "pid_old"}}
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

// TestChannelActor_Unregister_RemovesMember:注销只摘掉对应 RoleID,其余成员原地保留。
// 为什么需要:玩家断开/退会时只注销自己;误删整表会把还在线的玩家静默踢出频道推送。
func TestChannelActor_Unregister_RemovesMember(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	reg := func(id int64) {
		t.Helper()
		if _, err := a.HandleMessage(&pb.ChannelRegisterMsg{
			RoleId: id, Pid: &pb.ActorPid{Name: "p" + string(rune(id))},
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

// TestChannelActor_Unregister_LastMemberNoPanic:成员表清空后不 panic,落到 save + 30 分钟空闲回收定时器分支(WorldChannel 的 SaveInterval=0,save 直接早退)。
// 为什么需要:这段分支跑在注销请求的同步栈上;panic 会带走整个 actor 及其内存 buffer,频道历史永久丢失。
func TestChannelActor_Unregister_LastMemberNoPanic(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	if _, err := a.HandleMessage(&pb.ChannelRegisterMsg{
		RoleId: 5, Pid: &pb.ActorPid{Name: "pid5"},
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

// TestChannelActor_Send_EmptyContentRejected:空内容被 CanWrite 拒绝,不入 buffer,且 HandleMessage 仍返回 nil error。
// 为什么需要:拒绝要按 invariants #9 走错误载荷应答;若漏判,空消息会被存盘并广播给全体成员。
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

// TestChannelActor_Send_AppendsToBuffer:合法消息入 buffer,PChatMsg 带 Content 与非零的秒级 Timestamp。
// 为什么需要:Timestamp 由 actor 现场填充并作为落库排序依据,为 0 会让历史消息乱序。
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

// TestChannelActor_Send_WithMembersNoPanic:有成员在册时发送不 panic,消息照常入 buffer。
// 为什么需要:成员循环里的 PublishRoleNotify 是显式忽略错误的,任何 panic 都会带走整个 actor 与其内存 buffer。
// 注意:这里注册的 RoleID 为 0/-1,PublishRoleNotify 直接走 targetRoleID<=0 的 invalid 早退,owner 查找/notifyLocal/远端发布均未被触达。
func TestChannelActor_Send_WithMembersNoPanic(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	// RoleID<=0: PublishRoleNotify 走 invalid 分支(不触达未初始化的全局 Redis),
	// 测试聚焦"通知所有成员"流程不 panic + buffer 追加。
	for _, id := range []int64{0, -1} {
		if _, err := a.HandleMessage(&pb.ChannelRegisterMsg{
			RoleId: id, Pid: &pb.ActorPid{Name: "p" + string(rune(id))},
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

// TestChannelActor_History_ReturnsBuffered:发 3 条后经 **HandleCall** 取最近 2 条,
// 得到按时间正序的 [m2,m3];断言落在返回的 RspChatChannelHistory 载荷上,而不是直接
// 读 buffer。
// 为什么需要:ReqChatChannelHistory 是同步请求,只由 HandleCall 处理;若测试改走
// HandleMessage,switch 无此分支会静默 return nil,nil,断言就落空——历史查询路径
// 实际从未被执行,删掉整个 case 测试照样绿。
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

	// HandleCall 才是该请求的入口;返回值即客户端看到的响应载荷。
	rsp, err := a.HandleCall(&pb.ReqChatChannelHistory{
		ChannelType: 1, ChannelId: 100, Count: 2,
	})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	hist, ok := rsp.(*pb.RspChatChannelHistory)
	if !ok {
		t.Fatalf("expected *pb.RspChatChannelHistory, got %T", rsp)
	}
	if len(hist.Messages) != 2 || hist.Messages[0].Content != "m2" || hist.Messages[1].Content != "m3" {
		t.Fatalf("expected last 2 msgs in order, got %+v", hist.Messages)
	}
}

// TestChannelActor_History_CountClamped:count 传 0/-1/100000 时,经 HandleCall 必须返回
// *RspChatChannelHistory 且恰好带回全部 1 条已缓冲消息——不得 nil、不得报错、不得 panic。
// 为什么需要:count 完全来自客户端。响应若是 nil,客户端拿到一个空结构而非历史,
// 表现为"频道历史永远是空的"且服务端无任何报错。
// 注:HandleCall 的 clamp 与 ringBuffer.Recent 自身的越界保护当前是双层的——Recent
// 也会把 count 收敛到 len,所以这个用例断言的是**端到端响应正确性**;若将来去掉
// HandleCall 的 clamp,本用例仍应通过(Recent 那层兜住)。真正只由 HandleCall 承担的
// 是"把越界 count 挡在进 Recent 之前",当前无可观测差异。
func TestChannelActor_History_CountClamped(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	if _, err := a.HandleMessage(&pb.ReqChannelSend{
		ChannelType: 1, ChannelId: 100, SenderId: 5, Content: "x",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	for _, c := range []int32{0, -1, 100000} {
		rsp, err := a.HandleCall(&pb.ReqChatChannelHistory{
			ChannelType: 1, ChannelId: 100, Count: c,
		})
		if err != nil {
			t.Fatalf("history count=%d: %v", c, err)
		}
		hist, ok := rsp.(*pb.RspChatChannelHistory)
		if !ok {
			t.Fatalf("count=%d: expected *pb.RspChatChannelHistory, got %T", c, rsp)
		}
		if len(hist.Messages) != 1 || hist.Messages[0].Content != "x" {
			t.Fatalf("count=%d: expected the single buffered msg, got %+v", c, hist.Messages)
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

// 注意:本测试并未验证 Init 注册了周期落盘定时器。生产 Init 确有
// "SaveInterval()>0 则 AddTick(channel_save)"(channel_actor.go 的 Init 末段),
// 但 newTestChannelActor 在 Init 解析完 a.channel/a.buffer **之后**又用测试频道覆盖了
// a.channel,于是此处断言的只是注入常量的符号,恒为真。
// 真正要测的是"定时器有没有被注册",那需要断言 Timer 状态,不是断言 SaveInterval()。
func TestChannelActor_Init_WithSaveInterval(t *testing.T) {
	a := newTestChannelActor(t, GuildChannel{})
	if a.channel.SaveInterval() <= 0 {
		t.Fatal("test channel must have a positive save interval")
	}
}

// 同 _WithSaveInterval:SaveInterval()==0 的分支(Init 不注册 channel_save 定时器)
// 在这里同样没有被验证——helper 覆盖后该断言恒真。保留它是为了固定
// WorldChannel 配置 SaveInterval 为 0 这一前置事实,不是测 Init。
func TestChannelActor_Init_WithoutSaveInterval(t *testing.T) {
	a := newTestChannelActor(t, WorldChannel{})
	if a.channel.SaveInterval() != 0 {
		t.Fatal("test channel must have no save interval")
	}
}

// TestChannelActor_Terminate_NoPanic:对零定时器、db 为 nil 的 actor 调 Terminate 不得 panic。
// 为什么需要:Terminate 是 actor 停机路径,即使业务未初始化(如周期落盘定时器已随 actor
// 一并销毁)也必须安全返回。panic 会让停机变成进程崩溃,而不是干净的模块停机。
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
	a.key.Type = 4
	a.key.ID = 7

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
	a.key.Type = 4
	a.key.ID = 7

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
	a.key.Type = 4
	a.key.ID = 7

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
	a.key.Type = 4
	a.key.ID = 7

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
