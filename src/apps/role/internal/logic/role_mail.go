package logic

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	"gserver/core/gxypgx"
	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"
	"gserver/src/pkg/gameconfig"
)

// PersonalMailItem 个人邮件内容，状态保存在 RoleMailState.States。
type PersonalMailItem struct {
	ID          int64      `gorm:"primaryKey;column:id;type:bigint;default:nextval('mail_global_id_seq');autoIncrement:false"`
	RoleID      int64      `gorm:"column:role_id;index"`
	Title       string     `gorm:"column:title"`
	Summary     string     `gorm:"column:summary"`
	Content     string     `gorm:"column:content"`
	Attachments []bag.Good `gorm:"column:attachments;type:jsonb;serializer:json"`
	SendAt      int64      `gorm:"column:send_at"`
	ExpireAt    int64      `gorm:"column:expire_at"`
}

func (PersonalMailItem) TableName() string { return "personal_mail" }

type MailState struct {
	MailID    int64 `json:"mail_id"`
	IsSysMail bool  `json:"is_sys_mail,omitempty"`
	IsRead    bool  `json:"read,omitempty"`
	IsClaimed bool  `json:"claimed,omitempty"`
	IsDeleted bool  `json:"deleted,omitempty"`
}

type MailStateMap map[string]MailState

// RoleMailState 角色邮件状态，按全局邮件 ID 存 read/claimed/deleted。
type RoleMailState struct {
	RolePersistState
	LastSysMailID int64        `gorm:"column:last_sys_mail_id"`
	States        MailStateMap `gorm:"column:states;type:jsonb;serializer:json"`
}

func (RoleMailState) TableName() string { return "role_mail_state" }

func (r *RoleMailState) GetIndexes() []string {
	return []string{"update_at"}
}

// SysMailItem 全服邮件
type SysMailItem struct {
	ID          int64      `gorm:"primaryKey;column:id;type:bigint;default:nextval('mail_global_id_seq');autoIncrement:false"`
	Title       string     `gorm:"column:title"`
	Summary     string     `gorm:"column:summary"`
	Content     string     `gorm:"column:content"`
	Attachments []bag.Good `gorm:"column:attachments;type:jsonb;serializer:json"`
	SendAt      int64      `gorm:"column:create_at"`
	ExpireAt    int64      `gorm:"column:expire_at"`
}

func (SysMailItem) TableName() string { return "sys_mail" }

type MailView struct {
	ID          int64
	Title       string
	Summary     string
	Content     string
	Attachments []bag.Good
	SendAt      int64
	ExpireAt    int64
	IsRead      bool
	IsClaimed   bool
	IsDeleted   bool
}

func mailRuntimeConfig() *gamecfg.GardenMailConfig {
	return gameconfig.GameConfig().TbMailConfig.Get()
}

// RoleMail 模块
type RoleMail struct {
	RoleModule
	mailCache []MailView
	state     RoleMailState
}

var _ IRoleModule = (*RoleMail)(nil)

func (r *RoleMail) PersistState() IPersistState {
	return &r.state
}

func (r *RoleMail) AfterLogin(ctx context.Context) {
	if err := r.RefreshMailCache(ctx); err != nil {
		return
	}
}

func (r *RoleMail) RefreshMailCache(ctx context.Context) error {
	roleID := r.RoleID
	if r.state.States == nil {
		r.state.States = make(MailStateMap)
	}

	now := time.Now().Unix()
	config := mailRuntimeConfig()
	var personal []PersonalMailItem
	if err := gxypgx.DB().WithContext(ctx).
		Where("role_id = ?", roleID).
		Order("id DESC").
		Limit(int(config.MailMaxCount)).
		Find(&personal).Error; err != nil {
		return err
	}
	receivePersonalMails(&r.state, personal)

	var system []SysMailItem
	if err := gxypgx.DB().WithContext(ctx).
		Where("id > ? AND (expire_at = 0 OR expire_at >= ?)", r.state.LastSysMailID, now).
		Order("id ASC").
		Limit(int(config.MailMaxCount)).
		Find(&system).Error; err != nil {
		return err
	}
	receiveSystemMails(&r.state, system)

	visibleSystemIDs := systemMailIDsFromState(r.state.States)
	if len(visibleSystemIDs) > 0 {
		if err := gxypgx.DB().WithContext(ctx).
			Where("id IN ? AND (expire_at = 0 OR expire_at >= ?)", visibleSystemIDs, now).
			Find(&system).Error; err != nil {
			return err
		}
	}

	r.mailCache = buildMailViews(personal, system, r.state.States, now)
	return nil
}

func (r *RoleMail) OnCreate(ctx context.Context) {}

func (r *RoleMail) OnModStop(ctx context.Context) error { return nil }

func mailStateKey(id int64) string {
	return strconv.FormatInt(id, 10)
}

func applyMailState(view *MailView, states MailStateMap) {
	if states == nil {
		return
	}
	state := states[mailStateKey(view.ID)]
	view.IsRead = state.IsRead
	view.IsClaimed = state.IsClaimed
	view.IsDeleted = state.IsDeleted
}

func receiveSystemMails(state *RoleMailState, sysMails []SysMailItem) {
	if state.States == nil {
		state.States = make(MailStateMap)
	}
	for _, mail := range sysMails {
		if mail.ID <= state.LastSysMailID {
			continue
		}
		state.States[mailStateKey(mail.ID)] = MailState{
			MailID:    mail.ID,
			IsSysMail: true,
		}
		state.LastSysMailID = mail.ID
		state.MarkDirty()
	}
}

func receivePersonalMails(state *RoleMailState, personal []PersonalMailItem) {
	if state.States == nil {
		state.States = make(MailStateMap)
	}
	for _, mail := range personal {
		key := mailStateKey(mail.ID)
		mailState, ok := state.States[key]
		if ok {
			if mailState.MailID == 0 {
				mailState.MailID = mail.ID
				state.States[key] = mailState
				state.MarkDirty()
			}
			continue
		}
		state.States[key] = MailState{MailID: mail.ID}
		state.MarkDirty()
	}
}

func systemMailIDsFromState(states MailStateMap) []int64 {
	ids := make([]int64, 0, len(states))
	for _, state := range states {
		if state.IsSysMail && !state.IsDeleted {
			ids = append(ids, state.MailID)
		}
	}
	return ids
}

func buildMailViews(personal []PersonalMailItem, system []SysMailItem, states MailStateMap, now int64) []MailView {
	views := make([]MailView, 0, len(personal)+len(system))
	for _, m := range personal {
		view := MailView{
			ID:          m.ID,
			Title:       m.Title,
			Summary:     m.Summary,
			Content:     m.Content,
			Attachments: m.Attachments,
			SendAt:      m.SendAt,
			ExpireAt:    m.ExpireAt,
		}
		applyMailState(&view, states)
		if view.IsDeleted || (view.ExpireAt > 0 && view.ExpireAt < now) {
			continue
		}
		views = append(views, view)
	}
	for _, m := range system {
		view := MailView{
			ID:          m.ID,
			Title:       m.Title,
			Summary:     m.Summary,
			Content:     m.Content,
			Attachments: m.Attachments,
			SendAt:      m.SendAt,
			ExpireAt:    m.ExpireAt,
		}
		applyMailState(&view, states)
		if view.IsDeleted || (view.ExpireAt > 0 && view.ExpireAt < now) {
			continue
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].SendAt == views[j].SendAt {
			return views[i].ID > views[j].ID
		}
		return views[i].SendAt > views[j].SendAt
	})
	return trimMailViews(views, int(mailRuntimeConfig().MailMaxCount))
}

func trimMailViews(views []MailView, limit int) []MailView {
	if limit <= 0 || len(views) <= limit {
		return views
	}

	protected := make([]MailView, 0, limit)
	ordinary := make([]MailView, 0, len(views))
	for _, mail := range views {
		if len(mail.Attachments) > 0 && !mail.IsClaimed {
			protected = append(protected, mail)
			continue
		}
		ordinary = append(ordinary, mail)
	}

	if len(protected) >= limit {
		return protected[:limit]
	}

	trimmed := make([]MailView, 0, limit)
	trimmed = append(trimmed, protected...)
	remain := limit - len(trimmed)
	if len(ordinary) > remain {
		ordinary = ordinary[:remain]
	}
	trimmed = append(trimmed, ordinary...)
	return trimmed
}

func (r *RoleMail) saveMailState(mail *MailView) {
	if r.state.States == nil {
		r.state.States = make(MailStateMap)
	}
	key := mailStateKey(mail.ID)
	state := r.state.States[key]
	state.MailID = mail.ID
	state.IsRead = mail.IsRead
	state.IsClaimed = mail.IsClaimed
	state.IsDeleted = mail.IsDeleted
	r.state.States[key] = state
	r.state.MarkDirty()
}

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

func (r *RoleMail) findMail(id int64) *MailView {
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
		r.saveMailState(mail)
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
	r.saveMailState(mail)

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
	var claimedMails []*MailView
	claimLimit := int(mailRuntimeConfig().OneKeyClaimLimit)

	for i := range r.mailCache {
		if claimLimit > 0 && len(claimedMails) >= claimLimit {
			break
		}
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
		claimedMails = append(claimedMails, m)
	}

	if len(claimedMails) == 0 {
		return &pb.RspMailClaimAll{}, nil
	}

	if err := r.Role.Bag.SaveGoods(ctx, nil, allGoods, "mail_claim_all", bag.OptNotifyReward()); err != nil {
		return nil, err
	}

	for _, mail := range claimedMails {
		r.saveMailState(mail)
	}

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
		if !mailRuntimeConfig().AllowDeleteUnclaimed {
			return nil, errors.New("claim attachments before delete")
		}
	}

	var kept []MailView
	for _, m := range r.mailCache {
		if m.ID != req.MailId {
			kept = append(kept, m)
		}
	}
	mail.IsDeleted = true
	r.saveMailState(mail)
	r.mailCache = kept

	unread, unclaimed := r.calcRedDot()
	return &pb.RspMailDelete{
		UnreadCount:    unread,
		UnclaimedCount: unclaimed,
	}, nil
}
