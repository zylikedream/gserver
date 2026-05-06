package logic

import (
	"context"
	"errors"
	"time"

	"gserver/gameconfig"
	"gorm.io/gorm"
)

var (
	ErrSelfAdd         = errors.New("不能添加自己为好友")
	ErrAlreadyFriend   = errors.New("对方已经是好友")
	ErrFriendFull      = errors.New("好友数量已达上限")
	ErrApplyDuplicated = errors.New("已申请过，等待对方处理")
	ErrApplyNotFound   = errors.New("申请不存在或已过期")
	ErrCooldown        = errors.New("操作过于频繁，请稍后再试")
)

type Config struct {
	FriendMaxCount    int32
	ApplySendLimit    int32
	ApplyReceiveLimit int32
	DeleteReapplyCD   int32
}

func LoadConfig() *Config {
	tb := gameconfig.GameConfig().TbFriendConfig.Get()
	return &Config{
		FriendMaxCount:    tb.FriendMaxCount,
		ApplySendLimit:    tb.ApplySendLimit,
		ApplyReceiveLimit: tb.ApplyReceiveLimit,
		DeleteReapplyCD:   tb.DeleteReapplyCdSeconds,
	}
}

// lockBoth 按 player_id 顺序锁双方行，防死锁
// 返回的 first.PlayerID < second.PlayerID，调用方自行分配
func lockBoth(tx *gorm.DB, a, b int64) (first, second *FriendData, err error) {
	if a < b {
		first, err = lockRow(tx, a)
		if err != nil {
			return
		}
		second, err = lockRow(tx, b)
		return
	}
	first, err = lockRow(tx, b)
	if err != nil {
		return
	}
	second, err = lockRow(tx, a)
	return
}

// SendRequest 发起好友申请
func SendRequest(ctx context.Context, fromID, toID int64, cfg *Config) error {
	if fromID == toID {
		return ErrSelfAdd
	}

	tx := openTx(ctx)
	defer tx.Rollback()

	a, b, err := lockBoth(tx, fromID, toID)
	if err != nil {
		return err
	}

	// 分配谁是谁
	if a.PlayerID != fromID {
		a, b = b, a
	}
	me, target := a, b

	now := time.Now().Unix()

	for _, cd := range me.Cooldowns {
		if cd.TargetID == toID && cd.Until > now {
			return ErrCooldown
		}
	}
	if me.Friends.Has(toID) || target.Friends.Has(fromID) {
		return ErrAlreadyFriend
	}
	if me.Outgoing.Has(toID) {
		return ErrApplyDuplicated
	}
	if len(me.Outgoing) >= int(cfg.ApplySendLimit) {
		return errors.New("发送申请数量已达上限")
	}
	if len(target.Incoming) >= int(cfg.ApplyReceiveLimit) {
		return errors.New("对方申请列表已满")
	}

	me.Outgoing = append(me.Outgoing, ApplyEntry{PlayerID: toID, ApplyAt: now})
	target.Incoming = append(target.Incoming, ApplyEntry{PlayerID: fromID, ApplyAt: now})

	if err := saveRow(tx, me); err != nil {
		return err
	}
	if err := saveRow(tx, target); err != nil {
		return err
	}
	return tx.Commit().Error
}

// AcceptRequest 同意好友申请
func AcceptRequest(ctx context.Context, myID, fromID int64, cfg *Config) error {
	tx := openTx(ctx)
	defer tx.Rollback()

	a, b, err := lockBoth(tx, myID, fromID)
	if err != nil {
		return err
	}

	if a.PlayerID != myID {
		a, b = b, a
	}
	me, other := a, b

	if !me.Incoming.Has(fromID) {
		return ErrApplyNotFound
	}
	if len(me.Friends) >= int(cfg.FriendMaxCount) {
		return ErrFriendFull
	}
	if len(other.Friends) >= int(cfg.FriendMaxCount) {
		return errors.New("对方好友数量已达上限")
	}

	now := time.Now().Unix()
	me.Friends = append(me.Friends, FriendEntry{PlayerID: fromID, AddedAt: now})
	other.Friends = append(other.Friends, FriendEntry{PlayerID: myID, AddedAt: now})

	me.Incoming = me.Incoming.Remove(fromID)
	other.Outgoing = other.Outgoing.Remove(myID)

	if err := saveRow(tx, me); err != nil {
		return err
	}
	if err := saveRow(tx, other); err != nil {
		return err
	}
	return tx.Commit().Error
}

// RejectRequest 拒绝好友申请
func RejectRequest(ctx context.Context, myID, fromID int64) error {
	tx := openTx(ctx)
	defer tx.Rollback()

	a, b, err := lockBoth(tx, myID, fromID)
	if err != nil {
		return err
	}

	if a.PlayerID != myID {
		a, b = b, a
	}
	me, other := a, b

	if !me.Incoming.Has(fromID) {
		return ErrApplyNotFound
	}

	me.Incoming = me.Incoming.Remove(fromID)
	other.Outgoing = other.Outgoing.Remove(myID)

	if err := saveRow(tx, me); err != nil {
		return err
	}
	if err := saveRow(tx, other); err != nil {
		return err
	}
	return tx.Commit().Error
}

// RemoveFriend 删除好友
func RemoveFriend(ctx context.Context, myID, targetID int64, cfg *Config) error {
	tx := openTx(ctx)
	defer tx.Rollback()

	a, b, err := lockBoth(tx, myID, targetID)
	if err != nil {
		return err
	}

	if a.PlayerID != myID {
		a, b = b, a
	}
	me, other := a, b

	me.Friends = me.Friends.Remove(targetID)
	other.Friends = other.Friends.Remove(myID)

	cdUntil := time.Now().Unix() + int64(cfg.DeleteReapplyCD)
	me.Cooldowns = append(me.Cooldowns, CooldownEntry{TargetID: targetID, Until: cdUntil})
	other.Cooldowns = append(other.Cooldowns, CooldownEntry{TargetID: myID, Until: cdUntil})

	if err := saveRow(tx, me); err != nil {
		return err
	}
	if err := saveRow(tx, other); err != nil {
		return err
	}
	return tx.Commit().Error
}
