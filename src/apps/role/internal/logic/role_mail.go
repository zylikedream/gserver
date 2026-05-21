package logic

import (
	"context"
	"errors"
	"time"

	"gserver/core/gxylog"
	"gserver/core/gxypgx"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"
)

// MailEntry 单封邮件（role_mail_items 表）
type MailEntry struct {
	ID          int64      `gorm:"primaryKey"`
	RoleID      int64      `gorm:"column:role_id;index:idx_mail_role_id"`
	Title       string     `gorm:"column:title"`
	Summary     string     `gorm:"column:summary"`
	Content     string     `gorm:"column:content"`
	Attachments []bag.Good `gorm:"column:attachments;type:jsonb;serializer:json"`
	SendAt      int64      `gorm:"column:send_at"`
	ExpireAt    int64      `gorm:"column:expire_at"`
	IsRead      bool       `gorm:"column:is_read"`
	IsClaimed   bool       `gorm:"column:is_claimed"`
	IsSysMail   bool       `gorm:"column:is_sys_mail"`
	IsDeleted   bool       `gorm:"column:is_deleted"`
}

func (MailEntry) TableName() string { return "role_mail_items" }

// RoleMailMeta 角色邮件元数据
type RoleMailMeta struct {
	RolePersistState
	MaxID             int64 `gorm:"column:max_id"`
	LastExpandSysMail int64 `gorm:"column:last_expand_sys_mail_id"`
}

func (RoleMailMeta) TableName() string { return "role_mail_meta" }

func (r *RoleMailMeta) GetIndexes() []string {
	return []string{"update_at"}
}

// SysMailItem 全服邮件
type SysMailItem struct {
	ID          int64      `gorm:"primaryKey;autoIncrement"`
	Title       string     `gorm:"column:title"`
	Content     string     `gorm:"column:content"`
	Attachments []bag.Good `gorm:"column:attachments;type:jsonb;serializer:json"`
	ExpireAt    int64      `gorm:"column:expire_at"`
	CreateAt    int64      `gorm:"column:create_at"`
}

func (SysMailItem) TableName() string { return "sys_mail" }

// RoleMail 模块
type RoleMail struct {
	RoleModule
	mailCache []MailEntry
	meta      RoleMailMeta
}

var _ IRoleModule = (*RoleMail)(nil)

func (r *RoleMail) PersistState() IPersistState {
	return &r.meta
}

func (r *RoleMail) OnModInit(ctx context.Context) error {
	roleID := r.RoleID

	// 1. 加载玩家邮件列表（不含已删除）
	//    元数据由 role_main.loadModules 统一加载
	var mails []MailEntry
	if err := gxypgx.DB().WithContext(ctx).
		Where("role_id = ? AND is_deleted = false", roleID).
		Order("id DESC").
		Find(&mails).Error; err != nil {
		return err
	}
	r.mailCache = mails

	// 3. 确保 MaxID >= 已有邮件的最大 ID（处理外部发信）
	for _, m := range mails {
		if m.ID > r.meta.MaxID {
			r.meta.MaxID = m.ID
		}
	}

	// 4. 清理过期邮件
	r.cleanExpired(ctx)

	// 4. 展开全服邮件
	if err := r.expandSysMail(ctx); err != nil {
		gxylog.Warn(ctx, "expand sys mail failed", gxylog.Err(err))
	}

	return nil
}

func (r *RoleMail) AfterLogin(ctx context.Context) {
	if err := r.expandSysMail(ctx); err != nil {
		gxylog.Warn(ctx, "after login expand sys mail failed", gxylog.Err(err))
	}
}

func (r *RoleMail) cleanExpired(ctx context.Context) {
	now := time.Now().Unix()
	var expiredIDs []int64
	var kept []MailEntry
	for _, m := range r.mailCache {
		if m.ExpireAt > 0 && m.ExpireAt < now {
			expiredIDs = append(expiredIDs, m.ID)
		} else {
			kept = append(kept, m)
		}
	}
	if len(expiredIDs) > 0 {
		gxypgx.DB().WithContext(ctx).
			Model(&MailEntry{}).
			Where("id IN ?", expiredIDs).
			Update("is_deleted", true)
	}
	r.mailCache = kept
}

func (r *RoleMail) expandSysMail(ctx context.Context) error {
	var maxID int64
	if err := gxypgx.DB().WithContext(ctx).
		Model(&SysMailItem{}).
		Select("COALESCE(MAX(id), 0)").
		Scan(&maxID).Error; err != nil {
		return err
	}

	if r.meta.LastExpandSysMail >= maxID {
		return nil
	}

	var sysMails []SysMailItem
	if err := gxypgx.DB().WithContext(ctx).
		Where("id > ?", r.meta.LastExpandSysMail).
		Find(&sysMails).Error; err != nil {
		return err
	}

	for _, sm := range sysMails {
		entry := MailEntry{
			RoleID:      r.RoleID,
			Title:       sm.Title,
			Content:     sm.Content,
			Attachments: sm.Attachments,
			SendAt:      sm.CreateAt,
			ExpireAt:    sm.ExpireAt,
			IsSysMail:   true,
		}
		r.meta.MaxID++
		entry.ID = r.meta.MaxID

		if err := gxypgx.DB().WithContext(ctx).Create(&entry).Error; err != nil {
			return err
		}
		r.mailCache = append(r.mailCache, entry)
	}

	r.meta.LastExpandSysMail = maxID
	r.meta.MarkDirty()
	return nil
}

func (r *RoleMail) OnCreate(ctx context.Context) {}

func (r *RoleMail) OnModStop(ctx context.Context) error { return nil }

func (r *RoleMail) calcRedDot() (unread, unclaimed int32) {
	now := time.Now().Unix()
	for _, m := range r.mailCache {
		if m.ExpireAt > 0 && m.ExpireAt < now {
			continue
		}
		if !m.IsRead {
			unread++
		}
		if len(m.Attachments) > 0 && !m.IsClaimed {
			unclaimed++
		}
	}
	return
}

func (r *RoleMail) findMail(id int64) *MailEntry {
	for i := range r.mailCache {
		if r.mailCache[i].ID == id {
			return &r.mailCache[i]
		}
	}
	return nil
}

func (r *RoleMail) ReqMailList(ctx context.Context, req *pb.ReqMailList) (*pb.RspMailList, error) {
	now := time.Now().Unix()
	items := make([]*pb.PMailItem, 0, len(r.mailCache))
	for _, m := range r.mailCache {
		if m.ExpireAt > 0 && m.ExpireAt < now {
			continue
		}
		items = append(items, &pb.PMailItem{
			Id:            m.ID,
			Title:         m.Title,
			Summary:       m.Summary,
			SendAt:        m.SendAt,
			ExpireAt:      m.ExpireAt,
			IsRead:        m.IsRead,
			HasAttachment: len(m.Attachments) > 0,
			IsClaimed:     m.IsClaimed,
		})
	}
	unread, unclaimed := r.calcRedDot()
	return &pb.RspMailList{
		Mails:          items,
		UnreadCount:    unread,
		UnclaimedCount: unclaimed,
	}, nil
}

func (r *RoleMail) ReqMailDetail(ctx context.Context, req *pb.ReqMailDetail) (*pb.RspMailDetail, error) {
	mail := r.findMail(req.MailId)
	if mail == nil {
		return nil, errors.New("mail not found")
	}

	if !mail.IsRead {
		mail.IsRead = true
		gxypgx.DB().WithContext(ctx).
			Model(&MailEntry{}).
			Where("id = ?", req.MailId).
			Update("is_read", true)
	}

	attachments := make([]*pb.PGoodInfo, 0, len(mail.Attachments))
	for _, a := range mail.Attachments {
		attachments = append(attachments, &pb.PGoodInfo{
			PropId: int32(a.GoodID),
			Num:    int64(a.Num),
		})
	}

	return &pb.RspMailDetail{
		Mail: &pb.PMailDetail{
			Id:          mail.ID,
			Title:       mail.Title,
			Content:     mail.Content,
			SendAt:      mail.SendAt,
			ExpireAt:    mail.ExpireAt,
			Attachments: attachments,
			IsClaimed:   mail.IsClaimed,
		},
	}, nil
}

func (r *RoleMail) ReqMailClaim(ctx context.Context, req *pb.ReqMailClaim) (*pb.RspMailClaim, error) {
	mail := r.findMail(req.MailId)
	if mail == nil {
		return nil, errors.New("mail not found")
	}
	if mail.ExpireAt > 0 && mail.ExpireAt < time.Now().Unix() {
		return nil, errors.New("mail expired")
	}
	if len(mail.Attachments) == 0 {
		return nil, errors.New("no attachments")
	}
	if mail.IsClaimed {
		return nil, errors.New("already claimed")
	}

	goods := make([]*gamecfg.GardenGoodStack, 0, len(mail.Attachments))
	for _, a := range mail.Attachments {
		goods = append(goods, bag.MakeGoodStack(a.GoodID, int(a.Num)))
	}
	if err := r.Role.Bag.SaveGoods(ctx, nil, goods, "mail_claim", bag.OptNotifyReward()); err != nil {
		return nil, err
	}

	mail.IsClaimed = true
	gxypgx.DB().WithContext(ctx).
		Model(&MailEntry{}).
		Where("id = ?", req.MailId).
		Update("is_claimed", true)

	rewards := make([]*pb.PGoodInfo, 0, len(mail.Attachments))
	for _, a := range mail.Attachments {
		rewards = append(rewards, &pb.PGoodInfo{
			PropId: int32(a.GoodID),
			Num:    int64(a.Num),
		})
	}

	_, unclaimed := r.calcRedDot()
	return &pb.RspMailClaim{
		Rewards:        rewards,
		UnclaimedCount: unclaimed,
	}, nil
}

func (r *RoleMail) ReqMailClaimAll(ctx context.Context, req *pb.ReqMailClaimAll) (*pb.RspMailClaimAll, error) {
	now := time.Now().Unix()
	var allGoods []*gamecfg.GardenGoodStack
	var claimedIDs []int64

	for i := range r.mailCache {
		m := &r.mailCache[i]
		if m.ExpireAt > 0 && m.ExpireAt < now {
			continue
		}
		if len(m.Attachments) == 0 || m.IsClaimed {
			continue
		}
		for _, a := range m.Attachments {
			allGoods = append(allGoods, bag.MakeGoodStack(a.GoodID, int(a.Num)))
		}
		m.IsClaimed = true
		claimedIDs = append(claimedIDs, m.ID)
	}

	if len(claimedIDs) == 0 {
		return &pb.RspMailClaimAll{}, nil
	}

	if err := r.Role.Bag.SaveGoods(ctx, nil, allGoods, "mail_claim_all", bag.OptNotifyReward()); err != nil {
		return nil, err
	}

	gxypgx.DB().WithContext(ctx).
		Model(&MailEntry{}).
		Where("id IN ?", claimedIDs).
		Update("is_claimed", true)

	rewards := make([]*pb.PGoodInfo, 0, len(allGoods))
	for _, g := range allGoods {
		rewards = append(rewards, &pb.PGoodInfo{
			PropId: g.Id,
			Num:    int64(g.Num),
		})
	}

	_, unclaimed := r.calcRedDot()
	return &pb.RspMailClaimAll{
		Rewards:        rewards,
		UnclaimedCount: unclaimed,
	}, nil
}

func (r *RoleMail) ReqMailDelete(ctx context.Context, req *pb.ReqMailDelete) (*pb.RspMailDelete, error) {
	mail := r.findMail(req.MailId)
	if mail == nil {
		return nil, errors.New("mail not found")
	}
	if len(mail.Attachments) > 0 && !mail.IsClaimed {
		return nil, errors.New("claim attachments before delete")
	}

	var kept []MailEntry
	for _, m := range r.mailCache {
		if m.ID != req.MailId {
			kept = append(kept, m)
		}
	}
	r.mailCache = kept

	gxypgx.DB().WithContext(ctx).
		Model(&MailEntry{}).
		Where("id = ?", req.MailId).
		Update("is_deleted", true)

	unread, unclaimed := r.calcRedDot()
	return &pb.RspMailDelete{
		UnreadCount:    unread,
		UnclaimedCount: unclaimed,
	}, nil
}
