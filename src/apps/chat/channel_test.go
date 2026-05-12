package chat

import (
	"testing"
	"time"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
)

// ========== sortIDs ==========

func TestSortIDs_Ordered(t *testing.T) {
	a, b := sortIDs(1, 2)
	if a != 1 || b != 2 {
		t.Fatalf("expected (1,2), got (%d,%d)", a, b)
	}
}

func TestSortIDs_Reversed(t *testing.T) {
	a, b := sortIDs(5, 3)
	if a != 3 || b != 5 {
		t.Fatalf("expected (3,5), got (%d,%d)", a, b)
	}
}

func TestSortIDs_Equal(t *testing.T) {
	a, b := sortIDs(7, 7)
	if a != 7 || b != 7 {
		t.Fatalf("expected (7,7), got (%d,%d)", a, b)
	}
}

// ========== ringBuffer ==========

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

func TestRingBuffer_Recent_Zero(t *testing.T) {
	rb := newRingBuffer(5)
	rb.Push(&pb.PChatMsg{Content: "a"})
	msgs := rb.Recent(0)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 (0 means all), got %d", len(msgs))
	}
}

func TestRingBuffer_Recent_Empty(t *testing.T) {
	rb := newRingBuffer(5)
	msgs := rb.Recent(10)
	if len(msgs) != 0 {
		t.Fatalf("expected 0, got %d", len(msgs))
	}
}

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

func TestRingBuffer_Eviction_Many(t *testing.T) {
	rb := newRingBuffer(3)
	for i := 0; i < 10; i++ {
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

func TestWorldChannel_CanWrite(t *testing.T) {
	ch := WorldChannel{}
	if err := ch.CanWrite(1, "hello"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := ch.CanWrite(1, ""); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestWorldChannel_CanJoin(t *testing.T) {
	ch := WorldChannel{}
	if !ch.CanJoin(999) {
		t.Fatal("world channel should allow anyone")
	}
}

// ========== GuildChannel ==========

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
	if ch.TableName() != "guild_chat_log" {
		t.Fatalf("expected guild_chat_log, got %s", ch.TableName())
	}
}

func TestGuildChannel_CanWrite(t *testing.T) {
	ch := GuildChannel{}
	if err := ch.CanWrite(1, "hello"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := ch.CanWrite(1, ""); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestGuildChannel_CanJoin(t *testing.T) {
	ch := GuildChannel{}
	if !ch.CanJoin(123) {
		t.Fatal("guild channel should allow any member")
	}
}

// ========== GetChannel ==========

func TestGetChannel_World(t *testing.T) {
	ch, ok := GetChannel(int32(gamecfg.GardenEChatChannelType_WORLD))
	if !ok {
		t.Fatal("expected world channel")
	}
	if _, ok := ch.(WorldChannel); !ok {
		t.Fatal("expected WorldChannel type")
	}
}

func TestGetChannel_Guild(t *testing.T) {
	ch, ok := GetChannel(int32(gamecfg.GardenEChatChannelType_GUILD))
	if !ok {
		t.Fatal("expected guild channel")
	}
	if _, ok := ch.(GuildChannel); !ok {
		t.Fatal("expected GuildChannel type")
	}
}

func TestGetChannel_NotFound(t *testing.T) {
	_, ok := GetChannel(9999)
	if ok {
		t.Fatal("expected not found for unknown channel type")
	}
}
