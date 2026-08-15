package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"

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
		return errors.Wrapf(err, "channel actor init: invalid id %q", id)
	}
	ch, ok := GetChannel(a.ChannelType)
	if !ok {
		return errors.New("unknown channel type")
	}
	a.channel = ch
	a.buffer = newRingBuffer(ch.RingBufferSize())
	a.loadHistory(ctx)
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
			_ = gxyactor.Respond(ctx, a.Actx, gxyactor.ActorError(err.Error()))
			return nil
		}
		chatMsg := &pb.PChatMsg{
			Sender:    &pb.PRolePublic{RoleId: m.SenderId},
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
			_ = rolelib.PublishRoleNotify(ctx, mbr.RoleID, notify)
		}

	case *pb.ReqChatChannelHistory:
		count := int(m.Count)
		if count <= 0 || count > a.channel.RingBufferSize() {
			count = a.channel.RingBufferSize()
		}
		msgs := a.buffer.Recent(count)
		_ = gxyactor.Respond(ctx, a.Actx, &pb.RspChatChannelHistory{Messages: msgs})
	}
	return nil
}

func (a *ChannelActor) Terminate(ctx context.Context, err error) {
	a.save(ctx)
	_ = a.StopModule(ctx)
}

func (a *ChannelActor) TickSave(ctx context.Context, _ gxytimer.TimerActiveInfo) {
	a.save(ctx)
}

// loadHistory 启动时从落库表加载最近历史到内存 buffer。
// 仅对 SaveInterval>0 的频道生效; 加载量上限 = RingBufferSize。
// lastSavedSeq 对齐加载量, 避免后续 save 重复落库已加载的消息。
func (a *ChannelActor) loadHistory(ctx context.Context) {
	if a.channel == nil || a.channel.SaveInterval() <= 0 || a.channel.TableName() == "" || a.db == nil {
		return
	}
	type row struct {
		SenderID  int64  `gorm:"column:sender_id"`
		Content   string `gorm:"column:content"`
		Timestamp int64  `gorm:"column:timestamp"`
	}
	var rows []row
	if err := a.db.Table(a.channel.TableName()).
		Where("channel_type = ? AND channel_id = ?", a.ChannelType, a.ChannelID).
		Order("id DESC").
		Limit(a.channel.RingBufferSize()).
		Find(&rows).Error; err != nil {
		gxylog.Error(ctx, "load channel history failed",
			gxylog.Str("channel", fmt.Sprintf("%d_%d", a.ChannelType, a.ChannelID)),
			gxylog.Err(err))
		return
	}
	for i := len(rows) - 1; i >= 0; i-- { // DESC → 正序 Push
		r := rows[i]
		a.buffer.Push(&pb.PChatMsg{
			Sender:    &pb.PRolePublic{RoleId: r.SenderID},
			Content:   r.Content,
			Timestamp: r.Timestamp,
		})
	}
	a.lastSavedSeq = a.buffer.Len()
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
		if err := a.db.Table(a.channel.TableName()).Create(map[string]any{
			"channel_type": a.ChannelType,
			"channel_id":   a.ChannelID,
			"sender_id":    msg.Sender.GetRoleId(),
			"content":      msg.Content,
			"timestamp":    msg.Timestamp,
		}).Error; err != nil {
			gxylog.Error(ctx, "save channel msg failed",
				gxylog.Str("channel", fmt.Sprintf("%d_%d", a.ChannelType, a.ChannelID)),
				gxylog.Err(err))
			return // 保留 dirty, 下次重试
		}
	}
	a.lastSavedSeq = currentLen
}
