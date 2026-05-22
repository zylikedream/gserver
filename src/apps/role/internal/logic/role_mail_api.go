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
	"gserver/src/lib"
)

// SendMailOpts 发邮件选项
type SendMailOpts struct {
	Title       string
	Summary     string
	Content     string
	Attachments []bag.Good
	ExpireAt    int64 // 0 表示使用默认过期天数
}

func validateSendMailOpts(opts SendMailOpts, cfg *gamecfg.GardenMailConfig) error {
	if cfg == nil {
		cfg = mailRuntimeConfig()
	}
	if cfg.TitleLimit > 0 && len([]rune(opts.Title)) > int(cfg.TitleLimit) {
		return errors.New("mail title too long")
	}
	if cfg.ContentLimit > 0 && len([]rune(opts.Content)) > int(cfg.ContentLimit) {
		return errors.New("mail content too long")
	}
	return nil
}

// SendMail 给单个角色发邮件（外部系统调用，直接 INSERT DB）
func SendMail(ctx context.Context, roleID int64, opts SendMailOpts) error {
	config := mailRuntimeConfig()
	if err := validateSendMailOpts(opts, config); err != nil {
		return err
	}
	if opts.ExpireAt == 0 {
		opts.ExpireAt = time.Now().Unix() + int64(config.DefaultExpireDays)*86400
	}

	entry := PersonalMailItem{
		RoleID:      roleID,
		Title:       opts.Title,
		Summary:     opts.Summary,
		Content:     opts.Content,
		Attachments: opts.Attachments,
		SendAt:      time.Now().Unix(),
		ExpireAt:    opts.ExpireAt,
	}
	if err := gxypgx.DB().WithContext(ctx).Create(&entry).Error; err != nil {
		return err
	}

	notifyMailUpdate(ctx, roleID, entry.ID)
	return nil
}

// SendMailBatch 给多个角色发邮件（批量补偿等）
func SendMailBatch(ctx context.Context, roleIDs []int64, opts SendMailOpts) error {
	config := mailRuntimeConfig()
	if err := validateSendMailOpts(opts, config); err != nil {
		return err
	}
	if opts.ExpireAt == 0 {
		opts.ExpireAt = time.Now().Unix() + int64(config.DefaultExpireDays)*86400
	}

	now := time.Now().Unix()
	for _, roleID := range roleIDs {
		entry := PersonalMailItem{
			RoleID:      roleID,
			Title:       opts.Title,
			Summary:     opts.Summary,
			Content:     opts.Content,
			Attachments: opts.Attachments,
			SendAt:      now,
			ExpireAt:    opts.ExpireAt,
		}
		if err := gxypgx.DB().WithContext(ctx).Create(&entry).Error; err != nil {
			gxylog.Warn(ctx, "send mail batch create failed", gxylog.Num("roleID", roleID), gxylog.Err(err))
			continue
		}

		notifyMailUpdate(ctx, roleID, entry.ID)
	}
	return nil
}

// SendMailToAll 发全服邮件（写入 sys_mail，玩家登录时展开）
func SendMailToAll(ctx context.Context, opts SendMailOpts) error {
	config := mailRuntimeConfig()
	if err := validateSendMailOpts(opts, config); err != nil {
		return err
	}
	if opts.ExpireAt == 0 {
		opts.ExpireAt = time.Now().Unix() + int64(config.DefaultExpireDays)*86400
	}

	sysMail := SysMailItem{
		Title:       opts.Title,
		Summary:     opts.Summary,
		Content:     opts.Content,
		Attachments: opts.Attachments,
		ExpireAt:    opts.ExpireAt,
		SendAt:      time.Now().Unix(),
	}
	return gxypgx.DB().WithContext(ctx).Create(&sysMail).Error
}

// notifyMailUpdate 通知在线玩家（不强制激活 actor）
func notifyMailUpdate(ctx context.Context, roleID int64, mailID int64) {
	if err := lib.PublishRoleNotify(ctx, roleID, &pb.NotifyMailUpdate{MailId: mailID}); err != nil {
		gxylog.Warn(ctx, "notify mail update failed", gxylog.Num("roleID", roleID), gxylog.Err(err))
	}
}
