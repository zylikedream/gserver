package chat

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxypgx"
	"gserver/core/gxytimer"
	"gserver/protocol/pb"

	"github.com/asynkron/protoactor-go/actor"
)

// ringBuffer 线程安全的环形缓冲区
type ringBuffer struct {
	mu   sync.RWMutex
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
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if len(rb.msgs) >= rb.cap {
		rb.msgs = rb.msgs[1:]
	}
	rb.msgs = append(rb.msgs, msg)
	rb.seq++
	return rb.seq
}

func (rb *ringBuffer) Recent(count int) []*pb.PChatMsg {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	if count <= 0 || count > len(rb.msgs) {
		count = len(rb.msgs)
	}
	result := make([]*pb.PChatMsg, count)
	copy(result, rb.msgs[len(rb.msgs)-count:])
	return result
}

func (rb *ringBuffer) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return len(rb.msgs)
}

type ChannelActor struct {
	gxymodule.ModuleBase
	*gxyactor.ActorBase
	ChannelType  int32
	ChannelID    int64
	channel      IChannel
	members      map[int64]*actor.PID
	buffer       *ringBuffer
	lastSavedSeq int
}

func NewChannelActor() *ChannelActor {
	ctx := gxylog.NewContext(context.Background(), "channel")
	a := &ChannelActor{
		members: make(map[int64]*actor.PID),
	}
	a.ActorBase = gxyactor.NewActorBase(ctx, a)
	return a
}

func (a *ChannelActor) Init(ctx context.Context, args []any) error {
	if len(args) < 2 {
		return errors.New("channel actor init: need [channelType(int32), channelID(int64)]")
	}
	a.ChannelType = args[0].(int32)
	a.ChannelID = args[1].(int64)
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
		a.members[m.RoleId] = &actor.PID{
			Address: m.Pid.Address,
			Id:      m.Pid.Id,
		}

	case *pb.ChannelUnregisterMsg:
		delete(a.members, m.RoleId)

	case *pb.ReqChannelSend:
		if err := a.channel.CanWrite(m.SenderId, m.Content); err != nil {
			a.Respond(err)
			return nil
		}
		chatMsg := &pb.PChatMsg{
			Content:   m.Content,
			Timestamp: time.Now().Unix(),
		}
		a.buffer.Push(chatMsg)

		notify := &pb.NotifyChannelChat{
			ChannelType: m.ChannelType,
			ChannelId:   m.ChannelId,
			SenderId:    m.SenderId,
			Content:     m.Content,
			Timestamp:   chatMsg.Timestamp,
		}
		// 通知所有成员
		for roleID := range a.members {
			pid, err := gxyactor.ActivateActor("role", strconv.FormatInt(roleID, 10), false)
			if err == nil {
				a.Send(pid, notify)
			}
		}
		a.Respond(nil)

	case *pb.ReqChannelHistory:
		count := int(m.Count)
		if count <= 0 || count > a.channel.RingBufferSize() {
			count = a.channel.RingBufferSize()
		}
		msgs := a.buffer.Recent(count)
		a.Respond(&pb.RspChannelHistory{Messages: msgs})
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
	if a.channel.SaveInterval() <= 0 {
		return
	}
	currentLen := a.buffer.Len()
	if currentLen <= a.lastSavedSeq {
		return
	}
	msgs := a.buffer.Recent(currentLen - a.lastSavedSeq)
	for _, msg := range msgs {
		gxypgx.DB().Table(a.channel.TableName()).Create(map[string]any{
			"channel_type": a.ChannelType,
			"channel_id":   a.ChannelID,
			"sender_id":    0,
			"content":      msg.Content,
			"timestamp":    msg.Timestamp,
		})
	}
	a.lastSavedSeq = currentLen
}
