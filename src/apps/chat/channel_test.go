package chat

import (
	"testing"
	"time"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
)

// ========== sortIDs ==========

// TestSortIDs_Ordered:已升序的 (1,2) 原样返回。
// 为什么需要:(min,max) 是私聊会话在 chat_private_message 里的唯一键,写侧 StorePrivateMsg 与读侧 GetPrivateHistory 必须算出同一个值。
func TestSortIDs_Ordered(t *testing.T) {
	a, b := sortIDs(1, 2)
	if a != 1 || b != 2 {
		t.Fatalf("expected (1,2), got (%d,%d)", a, b)
	}
}

// TestSortIDs_Reversed:(5,3) 必须被归一成 (3,5)。
// 为什么需要:不归一化的话,B 发起的历史查询会用 (5,3) 去查 A 写入的 (3,5) 行,结果永远是空列表。
func TestSortIDs_Reversed(t *testing.T) {
	a, b := sortIDs(5, 3)
	if a != 3 || b != 5 {
		t.Fatalf("expected (3,5), got (%d,%d)", a, b)
	}
}

// TestSortIDs_Equal:相等 id 必须原样返回 (7,7),不能因"不大于"而被拒。
// 为什么需要:自聊是合法会话;这里返回错误会让自聊消息无处落库。
func TestSortIDs_Equal(t *testing.T) {
	a, b := sortIDs(7, 7)
	if a != 7 || b != 7 {
		t.Fatalf("expected (7,7), got (%d,%d)", a, b)
	}
}

// ========== ringBuffer ==========

// TestRingBuffer_PushAndLen:Len 反映当前存活条数,不反映累计写入量。
// 为什么需要:save 用 Len-lastSavedSeq 决定落库增量;若 Len 记的是累计数,已存过的消息会被反复写库。
func TestRingBuffer_PushAndLen(t *testing.T) {
	rb := newRingBuffer(5)
	if rb.Len() != 0 {
		t.Fatalf("expected 0, got %d", rb.Len())
	}
	rb.Push(&pb.PChatMsg{Content: "a"})
	rb.Push(&pb.PChatMsg{Content: "b"})
	if rb.Len() != 2 {
		t.Fatalf("expected 2, got %d", rb.Len())
	}
}

// TestRingBuffer_Recent_All:请求数大于存量时返回全部,且保持写入顺序 [a,c]。
// 为什么需要:历史请求的 count 来自客户端,越界时必须退化为"全量"而不是切片越界 panic。
func TestRingBuffer_Recent_All(t *testing.T) {
	rb := newRingBuffer(5)
	rb.Push(&pb.PChatMsg{Content: "a"})
	rb.Push(&pb.PChatMsg{Content: "b"})
	rb.Push(&pb.PChatMsg{Content: "c"})
	msgs := rb.Recent(10)
	if len(msgs) != 3 {
		t.Fatalf("expected 3, got %d", len(msgs))
	}
	if msgs[0].Content != "a" || msgs[2].Content != "c" {
		t.Fatalf("unexpected order: %v", msgs)
	}
}

// TestRingBuffer_Recent_Partial:请求 2 条、存 3 条时返回最后两条 [b,c],保持写入顺序。
// 为什么需要:partial 是历史查询的主路径,取错端(最旧两条)会让新进频道的玩家看不到刚发的消息。
func TestRingBuffer_Recent_Partial(t *testing.T) {
	rb := newRingBuffer(5)
	rb.Push(&pb.PChatMsg{Content: "a"})
	rb.Push(&pb.PChatMsg{Content: "b"})
	rb.Push(&pb.PChatMsg{Content: "c"})
	msgs := rb.Recent(2)
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[0].Content != "b" || msgs[1].Content != "c" {
		t.Fatalf("expected last 2, got %v", msgs)
	}
}

// TestRingBuffer_Recent_Zero:count<=0 表示"取全部",不是"取 0 条"。
// 为什么需要:客户端传 0 是常见的"默认全量"语义,取 0 条会让新玩家进频道看到空白历史。
func TestRingBuffer_Recent_Zero(t *testing.T) {
	rb := newRingBuffer(5)
	rb.Push(&pb.PChatMsg{Content: "a"})
	msgs := rb.Recent(0)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 (0 means all), got %d", len(msgs))
	}
}

// TestRingBuffer_Recent_Empty:空 buffer 上 Recent 返回空切片而非 nil 之外的越界。
// 为什么需要:频道刚建、尚无任何消息时就有客户端来拉历史,这里是最早的执行路径。
func TestRingBuffer_Recent_Empty(t *testing.T) {
	rb := newRingBuffer(5)
	msgs := rb.Recent(10)
	if len(msgs) != 0 {
		t.Fatalf("expected 0, got %d", len(msgs))
	}
}

// TestRingBuffer_Eviction:写满 cap=3 后再推一条,最旧的一条被丢弃,留存 [b,c,d]。
// 为什么需要:ringBuffer 的容量就是频道内存消息上限;若不淘汰,世界频道会被无限撑爆内存。
func TestRingBuffer_Eviction(t *testing.T) {
	rb := newRingBuffer(3)
	rb.Push(&pb.PChatMsg{Content: "a"})
	rb.Push(&pb.PChatMsg{Content: "b"})
	rb.Push(&pb.PChatMsg{Content: "c"})
	rb.Push(&pb.PChatMsg{Content: "d"})
	if rb.Len() != 3 {
		t.Fatalf("expected 3 after eviction, got %d", rb.Len())
	}
	msgs := rb.Recent(3)
	if msgs[0].Content != "b" || msgs[1].Content != "c" || msgs[2].Content != "d" {
		t.Fatalf("expected [b,c,d], got %v", msgs)
	}
}

// TestRingBuffer_Eviction_Many:连续推 10 条到 cap=3,只留最后 3 条 [h,i,j]。
// 为什么需要:_Eviction 只覆盖一次淘汰;这里覆盖反复淘汰,防止 slice 前移后把未满的缓冲也丢掉。
func TestRingBuffer_Eviction_Many(t *testing.T) {
	rb := newRingBuffer(3)
	for i := range 10 {
		rb.Push(&pb.PChatMsg{Content: string(rune('a' + i))})
	}
	if rb.Len() != 3 {
		t.Fatalf("expected 3, got %d", rb.Len())
	}
	msgs := rb.Recent(3)
	if msgs[0].Content != "h" || msgs[1].Content != "i" || msgs[2].Content != "j" {
		t.Fatalf("expected [h,i,j], got %v", msgs)
	}
}

// TestRingBuffer_PushReturnsSeq:Push 返回自增序号 1,2,3。
// 为什么需要:消息序号是存盘与拉取之间的对齐依据(lastSavedSeq),重复或跳号会导致消息漏落库或重复落库。
func TestRingBuffer_PushReturnsSeq(t *testing.T) {
	rb := newRingBuffer(5)
	s1 := rb.Push(&pb.PChatMsg{Content: "a"})
	s2 := rb.Push(&pb.PChatMsg{Content: "b"})
	s3 := rb.Push(&pb.PChatMsg{Content: "c"})
	if s1 != 1 || s2 != 2 || s3 != 3 {
		t.Fatalf("expected 1,2,3 got %d,%d,%d", s1, s2, s3)
	}
}

// ========== WorldChannel ==========

// TestWorldChannel_Interface:世界频道的四个常量:类型 "world"、RingBufferSize 200、SaveInterval 0(不落盘)、TableName 空。
// 为什么需要:SaveInterval=0 是世界频道"纯内存"的开关,改成非 0 会让无对应表的写库路径启动即报错。
func TestWorldChannel_Interface(t *testing.T) {
	var ch IChannel = WorldChannel{}
	if ch.ChannelType() != "world" {
		t.Fatalf("expected world, got %s", ch.ChannelType())
	}
	if ch.RingBufferSize() != 200 {
		t.Fatalf("expected 200, got %d", ch.RingBufferSize())
	}
	if ch.SaveInterval() != 0 {
		t.Fatalf("expected 0, got %v", ch.SaveInterval())
	}
	if ch.TableName() != "" {
		t.Fatalf("expected empty, got %s", ch.TableName())
	}
}

// TestWorldChannel_CanWrite:仅按 content 非空判定,与 sender 无关。
// 为什么需要:这是唯一的发言准入,漏判空内容会把空消息广播并入库。
func TestWorldChannel_CanWrite(t *testing.T) {
	ch := WorldChannel{}
	if err := ch.CanWrite(1, "hello"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := ch.CanWrite(1, ""); err == nil {
		t.Fatal("expected error for empty content")
	}
}

// TestWorldChannel_CanJoin:任何角色都能进世界频道。
// 为什么需要:世界频道是默认频道,误设为有限制会让玩家静默无法发言。
func TestWorldChannel_CanJoin(t *testing.T) {
	ch := WorldChannel{}
	if !ch.CanJoin(999) {
		t.Fatal("world channel should allow anyone")
	}
}

// ========== GuildChannel ==========

// TestGuildChannel_Interface:公会频道常量:类型 "guild"、RingBufferSize 500、SaveInterval 600s、TableName=chat_guild_message。
// 为什么需要:这四个值同时决定了内存上限、定时落盘与目标表;TableName 写错会写到一张不存在的表上,日志里只剩落库失败。
func TestGuildChannel_Interface(t *testing.T) {
	var ch IChannel = GuildChannel{}
	if ch.ChannelType() != "guild" {
		t.Fatalf("expected guild, got %s", ch.ChannelType())
	}
	if ch.RingBufferSize() != 500 {
		t.Fatalf("expected 500, got %d", ch.RingBufferSize())
	}
	if ch.SaveInterval() != 600*time.Second {
		t.Fatalf("expected 600s, got %v", ch.SaveInterval())
	}
	if ch.TableName() != chatGuildMessageTable {
		t.Fatalf("expected chat_guild_message, got %s", ch.TableName())
	}
}

// TestGuildChannel_CanWrite:同样仅按 content 非空判定。
// 为什么需要:公会频道与落盘强绑定,一条空消息入库就是一条脏行。
func TestGuildChannel_CanWrite(t *testing.T) {
	ch := GuildChannel{}
	if err := ch.CanWrite(1, "hello"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := ch.CanWrite(1, ""); err == nil {
		t.Fatal("expected error for empty content")
	}
}

// TestGuildChannel_CanJoin:当前实现对任何成员一律放行。
// 为什么需要:入帮校验若落在这里,放行失效会让非会员混进公会频道并把消息写进 guild 表。
func TestGuildChannel_CanJoin(t *testing.T) {
	ch := GuildChannel{}
	if !ch.CanJoin(123) {
		t.Fatal("guild channel should allow any member")
	}
}

// ========== GetChannel ==========

// TestGetChannel_World:策划枚举 WORLD 映射到 WorldChannel 实现。
// 为什么需要:channelRegistry 靠 gamecfg 枚举做键,枚举改名或错配会让 Init 直接返回 "unknown channel type"。
func TestGetChannel_World(t *testing.T) {
	ch, ok := GetChannel(int32(gamecfg.GardenEChatChannelType_WORLD))
	if !ok {
		t.Fatal("expected world channel")
	}
	if _, ok := ch.(WorldChannel); !ok {
		t.Fatal("expected WorldChannel type")
	}
}

// TestGetChannel_Guild:策划枚举 GUILD 映射到 GuildChannel 实现。
// 为什么需要:同上,错配会让整个公会频道功能静默失效。
func TestGetChannel_Guild(t *testing.T) {
	ch, ok := GetChannel(int32(gamecfg.GardenEChatChannelType_GUILD))
	if !ok {
		t.Fatal("expected guild channel")
	}
	if _, ok := ch.(GuildChannel); !ok {
		t.Fatal("expected GuildChannel type")
	}
}

// TestGetChannel_NotFound:未知类型必须返回 ok=false,而不是零值实现。
// 为什么需要:零值实现会让 CanWrite 静默放行,策划表里新增一个未实现的频道类型时问题完全不可见。
func TestGetChannel_NotFound(t *testing.T) {
	_, ok := GetChannel(9999)
	if ok {
		t.Fatal("expected not found for unknown channel type")
	}
}
