package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxypgx"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"
	"gserver/src/lib/rolelib"

	"github.com/asynkron/protoactor-go/actor"

	"gorm.io/gorm"
)

const stopTimerName = "channel_stop"

// ringBuffer 线程安全的环形缓冲区
type ringBuffer struct {
	msgs []*pb.PChatMsg
	cap  int
	seq  int
}

func newRingBuffer(cap int) *ringBuffer {
	return &ringBuffer{
		msgs: make([]*pb.PChatMsg, 0, cap),
		cap:  cap,
	}
}

func (rb *ringBuffer) Push(msg *pb.PChatMsg) int {
	if len(rb.msgs) >= rb.cap {
		rb.msgs = rb.msgs[1:]
	}
	rb.msgs = append(rb.msgs, msg)
	rb.seq++
	return rb.seq
}

func (rb *ringBuffer) Recent(count int) []*pb.PChatMsg {
	if count <= 0 || count > len(rb.msgs) {
		count = len(rb.msgs)
	}
	result := make([]*pb.PChatMsg, count)
	copy(result, rb.msgs[len(rb.msgs)-count:])
	return result
}

func (rb *ringBuffer) Len() int {
	return len(rb.msgs)
}

type channelMember struct {
	Pid      *actor.PID
	RoleID   int64
	JoinTime time.Time
}

type ChannelActor struct {
	gxymodule.ModuleBase
	*gxyactor.ActorBase
	ChannelType  int32
	ChannelID    int64
	channel      IChannel
	members      map[int64]*channelMember
	buffer       *ringBuffer
	lastSavedSeq int
	db           *gorm.DB
}

func NewChannelActor() *ChannelActor {
	ctx := gxylog.NewContext(context.Background(), "channel")
	a := &ChannelActor{
		members: make(map[int64]*channelMember),
		db:      gxypgx.DB(),
	}
	a.ActorBase = gxyactor.NewActorBase(ctx, a, "channel")
	return a
}

func (a *ChannelActor) Init(ctx context.Context, args []any) error {
	// 从 actor name（"channelType_channelID"）解析频道类型和 ID
	if len(args) < 1 {
		return errors.New("channel actor init: need channelType_channelID]")
	}
	id := args[0].(string)
	_, err := fmt.Sscanf(id, "%d_%d", &a.ChannelType, &a.ChannelID)
	if err != nil {
		return fmt.Errorf("channel actor init: invalid id %q: %w", id, err)
	}
	ch, ok := GetChannel(a.ChannelType)
	if !ok {
		return errors.New("unknown channel type")
	}
	a.channel = ch
	a.buffer = newRingBuffer(ch.RingBufferSize())
	return nil
}

func (a *ChannelActor) DelayInit(ctx context.Context) error {
	if a.channel.SaveInterval() > 0 {
		a.Timer().AddTick(ctx, &gxytimer.Tick{
			Name:     "channel_save",
			Interval: a.channel.SaveInterval(),
		}, a.TickSave)
	}
	return nil
}

func (a *ChannelActor) HandleMessage(ctx context.Context, msg any) error {
	switch m := msg.(type) {
	case *pb.ChannelRegisterMsg:
		pid := &actor.PID{
			Address: m.Pid.Address,
			Id:      m.Pid.Id,
		}
		a.members[m.RoleId] = &channelMember{
			Pid:      pid,
			RoleID:   m.RoleId,
			JoinTime: time.Now(),
		}
		a.Timer().Cancel(ctx, stopTimerName)

	case *pb.ChannelUnregisterMsg:
		delete(a.members, m.RoleId)
		if len(a.members) == 0 {
			a.save(ctx)
			a.Timer().AddOnce(ctx, &gxytimer.Once{
				Name:  stopTimerName,
				After: 30 * time.Minute,
			}, func(_ context.Context, _ gxytimer.TimerActiveInfo) {
				if len(a.members) == 0 {
					a.Stop(nil)
				}
			})
		}

	case *pb.ReqChannelSend:
		if err := a.channel.CanWrite(m.SenderId, m.Content); err != nil {
			gxyactor.Respond(ctx, a.Actx, gxyactor.ActorError(err.Error()))
			return nil
		}
		chatMsg := &pb.PChatMsg{
			Content:   m.Content,
			Timestamp: time.Now().Unix(),
		}
		a.buffer.Push(chatMsg)

		notify := &pb.NotifyChatChannel{
			ChannelType: m.ChannelType,
			ChannelId:   m.ChannelId,
			SenderId:    m.SenderId,
			Content:     m.Content,
			Timestamp:   chatMsg.Timestamp,
		}
		// 通知所有成员
		for _, mbr := range a.members {
			rolelib.PublishRoleNotify(ctx, mbr.RoleID, notify)
		}

	case *pb.ReqChatChannelHistory:
		count := int(m.Count)
		if count <= 0 || count > a.channel.RingBufferSize() {
			count = a.channel.RingBufferSize()
		}
		msgs := a.buffer.Recent(count)
		gxyactor.Respond(ctx, a.Actx, &pb.RspChatChannelHistory{Messages: msgs})
	}
	return nil
}

func (a *ChannelActor) Terminate(ctx context.Context, err error) {
	a.save(ctx)
	a.StopModule(ctx)
}

func (a *ChannelActor) TickSave(ctx context.Context, _ gxytimer.TimerActiveInfo) {
	a.save(ctx)
}

func (a *ChannelActor) save(ctx context.Context) {
	if a.channel == nil || a.channel.SaveInterval() <= 0 {
		return
	}
	currentLen := a.buffer.Len()
	if currentLen <= a.lastSavedSeq {
		return
	}
	msgs := a.buffer.Recent(currentLen - a.lastSavedSeq)
	for _, msg := range msgs {
		a.db.Table(a.channel.TableName()).Create(map[string]any{
			"channel_type": a.ChannelType,
			"channel_id":   a.ChannelID,
			"sender_id":    0,
			"content":      msg.Content,
			"timestamp":    msg.Timestamp,
		})
	}
	a.lastSavedSeq = currentLen
}
