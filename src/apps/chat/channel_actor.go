package chat

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/cockroachdb/errors"

	"gserver/core/gxyactor"
	"gserver/core/gxylog"
	"gserver/core/gxymodule"
	"gserver/core/gxypgx"
	"gserver/protocol/pb"
	"gserver/src/lib/rolelib"

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
	RoleID   int64
	JoinTime time.Time
}

type ChannelActor struct {
	gxymodule.ModuleBase
	*gxyactor.EntityActor
	ChannelType  int32
	ChannelID    int64
	channel      IChannel
	members      map[int64]*channelMember
	buffer       *ringBuffer
	lastSavedSeq int
	db           *gorm.DB
}

func NewChannelActor() *ChannelActor {
	a := &ChannelActor{
		members: make(map[int64]*channelMember),
		db:      gxypgx.DB(),
	}
	a.EntityActor = gxyactor.NewEntityActor()
	return a
}

func (a *ChannelActor) Init(args ...any) error {
	// 先校验参数:不合法时不必惊动所有权协调层,否则会为一次注定失败的创建
	// 先占一份归属再回滚。
	if len(args) < 1 {
		return errors.New("channel actor init: need channelType_channelID]")
	}
	id, _ := args[0].(string)
	if _, err := fmt.Sscanf(id, "%d_%d", &a.ChannelType, &a.ChannelID); err != nil {
		return errors.Wrapf(err, "channel actor init: invalid id %q", id)
	}
	ch, ok := GetChannel(a.ChannelType)
	if !ok {
		return errors.New("unknown channel type")
	}
	ctx := a.Ctx
	a.channel = ch
	a.buffer = newRingBuffer(ch.RingBufferSize())
	a.loadHistory(ctx)
	if a.channel.SaveInterval() > 0 {
		a.Timer().AddTick("channel_save", a.channel.SaveInterval(), a.TickSave)
	}
	return nil
}

// HandleMessage 是业务入口(异步消息)。
//
// 异步路径上业务失败不能返回 error(那会终止本 actor),因此以错误载荷应答,
// 见 invariants #9。
func (a *ChannelActor) HandleMessage(msg any) (any, error) {
	ctx := a.Ctx
	switch m := msg.(type) {
	case *pb.ChannelRegisterMsg:
		a.members[m.RoleId] = &channelMember{
			RoleID:   m.RoleId,
			JoinTime: time.Now(),
		}
		a.Timer().Cancel(ctx, stopTimerName)

	case *pb.ChannelUnregisterMsg:
		delete(a.members, m.RoleId)
		if len(a.members) == 0 {
			a.save(ctx)
			a.Timer().AddOnce(stopTimerName, 30*time.Minute, func(_ context.Context) {
				if len(a.members) == 0 {
					a.Stop(nil)
				}
			})
		}

	case *pb.ReqChannelSend:
		if err := a.channel.CanWrite(m.SenderId, m.Content); err != nil {
			return gxyactor.ActorError(err.Error()), nil
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
	}
	return nil, nil
}

// HandleCall 是业务入口(同步请求)。
func (a *ChannelActor) HandleCall(msg any) (any, error) {
	switch m := msg.(type) {
	case *pb.ReqChatChannelHistory:
		count := int(m.Count)
		if count <= 0 || count > a.channel.RingBufferSize() {
			count = a.channel.RingBufferSize()
		}
		return &pb.RspChatChannelHistory{Messages: a.buffer.Recent(count)}, nil
	}
	return nil, nil
}

// Terminate 是终止路径:最终落盘。
// 归属释放与定时器停止由门面在本方法返回后执行。
func (a *ChannelActor) Terminate(err error) {
	a.save(a.Ctx)
}

func (a *ChannelActor) TickSave(ctx context.Context) {
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
	for _, r := range slices.Backward(rows) { // DESC → 正序 Push

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
