# 邮件系统 MVP 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现邮件系统首版，支持系统发邮件、全服邮件、附件领取、红点提醒

**Architecture:** RoleMail 模块 + 独立行表 role_mail_items + sys_mail 全服邮件表。发邮件直接 INSERT DB，不需要激活 actor。新邮件通知通过 PublishRoleNotify 推送在线玩家。

**Tech Stack:** Go, protoactor-go, gorm, protobuf, gxypgx

---

### Task 1: 创建 protobuf 协议文件 mail.proto

**Files:**
- Create: `protocol/client/mail.proto`

- [ ] **Step 1: 创建 mail.proto**

```proto
// ID: 30001~30099
syntax = "proto3";
option go_package="./pb;pb";
package galaxy.protocol;

import "msg_options.proto";

message ReqMailList {
    option (msg_id) = 30001;
}

message RspMailList {
    option (msg_id) = 30002;
    repeated PMailItem mails = 1;
    int32 unread_count = 2;
    int32 unclaimed_count = 3;
}

message PMailItem {
    int64 id = 1;
    string title = 2;
    string summary = 3;
    int64 send_at = 4;
    int64 expire_at = 5;
    bool is_read = 6;
    bool has_attachment = 7;
    bool is_claimed = 8;
}

message ReqMailDetail {
    option (msg_id) = 30003;
    int64 mail_id = 1;
}

message RspMailDetail {
    option (msg_id) = 30004;
    PMailDetail mail = 1;
}

message PMailDetail {
    int64 id = 1;
    string title = 2;
    string content = 3;
    int64 send_at = 4;
    int64 expire_at = 5;
    repeated PGoodInfo attachments = 6;
    bool is_claimed = 7;
}

message ReqMailClaim {
    option (msg_id) = 30005;
    int64 mail_id = 1;
}

message RspMailClaim {
    option (msg_id) = 30006;
    repeated PGoodInfo rewards = 1;
    int32 unclaimed_count = 2;
}

message ReqMailClaimAll {
    option (msg_id) = 30007;
}

message RspMailClaimAll {
    option (msg_id) = 30008;
    repeated PGoodInfo rewards = 1;
    int32 unclaimed_count = 2;
}

message ReqMailDelete {
    option (msg_id) = 30009;
    int64 mail_id = 1;
}

message RspMailDelete {
    option (msg_id) = 30010;
    int32 unread_count = 2;
    int32 unclaimed_count = 3;
}

message NotifyMailUpdate {
    int32 unread_count = 1;
    int32 unclaimed_count = 2;
}
```

Since `PGoodInfo` and `NotifyBagReward` are already in `bag.proto` and referenced as reward display, no need to redefine them.

- [ ] **Step 2: 生成 protobuf 代码**

```bash
make pb
```
Expected: `protocol/pb/mail.pb.go` 被生成，文件内包含所有 message 类型。

- [ ] **Step 3: 提交**

```bash
git add protocol/client/mail.proto protocol/pb/mail.pb.go
git commit -m "feat(mail): add mail proto files"
```

---

### Task 2: 创建数据模型和 Schema

**Files:**
- Modify: `src/apps/role/internal/logic/role_schema.go`（加 AutoMigrate）
- Create: `src/apps/role/internal/logic/role_mail.go`（MailEntry, RoleMailMeta, SysMailItem, RoleMail 结构体）

- [ ] **Step 1: 在 role_mail.go 中定义数据模型**

```go
package logic

import (
    "gserver/src/apps/role/internal/logic/bag"
    "gorm.io/gorm"
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
```

- [ ] **Step 2: 定义 RoleMail 模块结构体**

在同一个文件末尾添加：

```go
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
    // Task 3 中填充
    return nil
}

func (r *RoleMail) AfterLogin(ctx context.Context) {
    // Task 3 中填充
}

func (r *RoleMail) OnCreate(ctx context.Context) {}

func (r *RoleMail) OnModStop(ctx context.Context) error { return nil }
```

- [ ] **Step 3: 在 role_schema.go 中添加 AutoMigrate**

在 `InitRoleSchema` 函数中追加：

```go
&MailEntry{},
&RoleMailMeta{},
&SysMailItem{},
```

编辑 `src/apps/role/internal/logic/role_schema.go`：

```go
if err := db.AutoMigrate(
    // ... existing ...
    &RoleChatState{},
    &MailEntry{},
    &RoleMailMeta{},
    &SysMailItem{},
); err != nil {
```

- [ ] **Step 4: 在 roleModules 中注册 RoleMail**

编辑 `src/apps/role/internal/logic/role_main.go`，在 `roleModules` 结构体中添加字段：

```go
type roleModules struct {
    // ... existing ...
    Mail  *RoleMail
}
```

- [ ] **Step 5: 提交**

```bash
git add src/apps/role/internal/logic/role_mail.go src/apps/role/internal/logic/role_schema.go src/apps/role/internal/logic/role_main.go
git commit -m "feat(mail): add data model and register RoleMail module"
```

---

### Task 3: 实现模块初始化和数据加载

**Files:**
- Modify: `src/apps/role/internal/logic/role_mail.go`

- [ ] **Step 1: 添加 OnModInit 实现**

在 `role_mail.go` 中补全 `OnModInit`：

```go
import (
    "context"
    "time"

    "gserver/core/gxylog"
    "gserver/core/gxypgx"
)

func (r *RoleMail) OnModInit(ctx context.Context) error {
    roleID := r.RoleID

    // 1. 加载元数据
    if err := loadModuleState(ctx, roleID, &r.meta); err != nil {
        return err
    }
    r.meta.SetRoleID(roleID)

    // 2. 加载玩家邮件列表（不含已删除）
    var mails []MailEntry
    if err := gxypgx.DB().WithContext(ctx).
        Where("role_id = ? AND is_deleted = false", roleID).
        Order("id DESC").
        Find(&mails).Error; err != nil {
        return err
    }
    r.mailCache = mails

    // 3. 清理过期邮件
    r.cleanExpired(ctx)

    // 4. 展开全服邮件
    if err := r.expandSysMail(ctx); err != nil {
        gxylog.Warn(ctx, "expand sys mail failed", gxylog.Err(err))
    }

    return nil
}
```

- [ ] **Step 2: 添加过期清理**

```go
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
```


- [ ] **Step 3: 添加全服邮件展开**

```go
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
        // 分配ID
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
```

- [ ] **Step 4: 添加 AfterLogin**

```go
func (r *RoleMail) AfterLogin(ctx context.Context) {
    if err := r.expandSysMail(ctx); err != nil {
        gxylog.Warn(ctx, "after login expand sys mail failed", gxylog.Err(err))
    }
}
```

- [ ] **Step 5: 编译验证**

```bash
go build ./...
```
Expected: 编译成功。

- [ ] **Step 6: 提交**

```bash
git add src/apps/role/internal/logic/role_mail.go
git commit -m "feat(mail): implement module init, sys mail expansion and expiration cleanup"
```

---

### Task 4: 实现协议处理器

**Files:**
- Modify: `src/apps/role/internal/logic/role_mail.go`

- [ ] **Step 1: 添加红点计算辅助方法**

```go
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
```

- [ ] **Step 2: 实现 ReqMailList**

```go
func (r *RoleMail) ReqMailList(ctx context.Context, req *pb.ReqMailList) (*pb.RspMailList, error) {
    now := time.Now().Unix()
    items := make([]*pb.PMailItem, 0, len(r.mailCache))
    for _, m := range r.mailCache {
        if m.ExpireAt > 0 && m.ExpireAt < now {
            continue
        }
        items = append(items, &pb.PMailItem{
            Id:             m.ID,
            Title:          m.Title,
            Summary:        m.Summary,
            SendAt:         m.SendAt,
            ExpireAt:       m.ExpireAt,
            IsRead:         m.IsRead,
            HasAttachment:  len(m.Attachments) > 0,
            IsClaimed:      m.IsClaimed,
        })
    }
    unread, unclaimed := r.calcRedDot()
    return &pb.RspMailList{
        Mails:          items,
        UnreadCount:    unread,
        UnclaimedCount: unclaimed,
    }, nil
}
```

- [ ] **Step 3: 实现 ReqMailDetail**

```go
func (r *RoleMail) ReqMailDetail(ctx context.Context, req *pb.ReqMailDetail) (*pb.RspMailDetail, error) {
    mail := r.findMail(req.MailId)
    if mail == nil {
        return nil, errors.New("mail not found")
    }

    // 标记已读
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

func (r *RoleMail) findMail(id int64) *MailEntry {
    for i := range r.mailCache {
        if r.mailCache[i].ID == id {
            return &r.mailCache[i]
        }
    }
    return nil
}
```

- [ ] **Step 4: 实现 ReqMailClaim**

```go
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

    // 发放奖励
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
```

- [ ] **Step 5: 实现 ReqMailClaimAll**

```go
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
```

- [ ] **Step 6: 实现 ReqMailDelete**

```go
func (r *RoleMail) ReqMailDelete(ctx context.Context, req *pb.ReqMailDelete) (*pb.RspMailDelete, error) {
    mail := r.findMail(req.MailId)
    if mail == nil {
        return nil, errors.New("mail not found")
    }
    if len(mail.Attachments) > 0 && !mail.IsClaimed {
        return nil, errors.New("claim attachments before delete")
    }

    // 从 cache 移除
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
```

- [ ] **Step 7: 编译验证**

```bash
go build ./...
```
Expected: 编译成功。

- [ ] **Step 8: 提交**

```bash
git add src/apps/role/internal/logic/role_mail.go
git commit -m "feat(mail): implement protocol handlers (list, detail, claim, claim_all, delete)"
```

---

### Task 5: 实现发邮件 API 和 GM 命令

**Files:**
- Modify: `src/apps/role/internal/logic/role_mail.go`（SendMail 内部 API）
- Modify: `src/apps/role/internal/logic/role_gm.go`（GM 命令）
- Create: `src/apps/role/internal/logic/role_mail_api.go`（导出发邮件 API）

- [ ] **Step 1: 创建导出发邮件 API 文件**

`src/apps/role/internal/logic/role_mail_api.go`：

```go
package logic

import (
    "context"
    "time"

    "gserver/core/gxylog"
    "gserver/core/gxypgx"
    "gserver/protocol/pb"
    "gserver/src/apps/role/internal/logic/bag"
    "gserver/src/lib"
)

type SendMailOpts struct {
    Title       string
    Summary     string
    Content     string
    Attachments []bag.Good
    ExpireAt    int64 // 0 = 使用默认过期天数
}

// SendMail 给单个玩家发邮件（直接 INSERT DB，外部系统调用）
func SendMail(ctx context.Context, roleID int64, opts SendMailOpts) error {
    
    expireAt := opts.ExpireAt
    if expireAt == 0 {
        expireAt = time.Now().Add(mailDefaultExpireDays * 24 * time.Hour).Unix()
    }

    // 获取自增ID（通过 role_mail_meta）
    var meta RoleMailMeta
    meta.SetRoleID(roleID)
    if err := loadModuleState(ctx, roleID, &meta); err != nil {
        return err
    }
    meta.MaxID++

    entry := MailEntry{
        ID:          meta.MaxID,
        RoleID:      roleID,
        Title:       opts.Title,
        Summary:     opts.Summary,
        Content:     opts.Content,
        Attachments: opts.Attachments,
        SendAt:      time.Now().Unix(),
        ExpireAt:    expireAt,
    }

    txErr := gxypgx.DB().Transaction(func(tx *gorm.DB) error {
        if err := tx.Create(&entry).Error; err != nil {
            return err
        }
        // 更新 meta
        return tx.Model(&meta).
            Where("role_id = ? AND max_id < ?", roleID, meta.MaxID).
            Update("max_id", meta.MaxID).Error
    })
    if txErr != nil {
        return txErr
    }

    // 通知在线玩家
    notifyMailUpdate(ctx, roleID)
    return nil
}

// SendMailBatch 给多个玩家发邮件
func SendMailBatch(ctx context.Context, roleIDs []int64, opts SendMailOpts) error {
    for _, id := range roleIDs {
        if err := SendMail(ctx, id, opts); err != nil {
            gxylog.Warn(ctx, "send mail batch failed", gxylog.Num("roleID", id), gxylog.Err(err))
        }
    }
    return nil
}

// SendMailToAll 发全服邮件
func SendMailToAll(ctx context.Context, opts SendMailOpts) error {
    
    expireAt := opts.ExpireAt
    if expireAt == 0 {
        expireAt = time.Now().Add(mailDefaultExpireDays * 24 * time.Hour).Unix()
    }

    sysMail := SysMailItem{
        Title:       opts.Title,
        Content:     opts.Content,
        Attachments: opts.Attachments,
        ExpireAt:    expireAt,
        CreateAt:    time.Now().Unix(),
    }
    return gxypgx.DB().Create(&sysMail).Error
}

func notifyMailUpdate(ctx context.Context, roleID int64) {
    rolelib.PublishRoleNotify(ctx, roleID, &pb.NotifyMailUpdate{})
}
```

- [ ] **Step 2: 在 role_gm.go 添加 GM 命令**

```go
// 用法: send_mail [RoleID] [标题] [内容]
// 发送个人邮件（无附件）
func (r *RoleGM) SendMail(targetID int64, title, content string) error {
    return logic.SendMail(r.ctx, targetID, logic.SendMailOpts{
        Title:   title,
        Content: content,
    })
}

// 用法: send_mail_all [标题] [内容]
// 发送全服邮件（无附件）
func (r *RoleGM) SendMailAll(title, content string) error {
    return logic.SendMailToAll(r.ctx, logic.SendMailOpts{
        Title:   title,
        Content: content,
    })
}

// 用法: send_mail_goods [RoleID] [标题] [内容] [GoodID:Num,GoodID:Num]
// 发送带附件的邮件
func (r *RoleGM) SendMailGoods(targetID int64, title, content, goodsStr string) error {
    var goods []bag.Good
    for _, part := range strings.Split(goodsStr, ",") {
        part = strings.TrimSpace(part)
        if part == "" {
            continue
        }
        kv := strings.Split(part, ":")
        if len(kv) != 2 {
            continue
        }
        gid := gconv.Int(kv[0])
        num := gconv.Int(kv[1])
        goods = append(goods, bag.Good{GoodID: gid, Num: uint64(num)})
    }
    return logic.SendMail(r.ctx, targetID, logic.SendMailOpts{
        Title:       title,
        Content:     content,
        Attachments: goods,
    })
}
```

注意：需要在 role_gm.go 顶部 import 区域添加：
- `"gserver/src/apps/role/internal/logic/bag"`（但已在同一包内，按项目惯例直接使用）

Wait — `RoleGM` 和 `RoleMail` 在同一个 `logic` 包中，所以不需要 `logic.` 前缀，直接调函数就行。

修正 GM 命令：

```go
// 用法: send_mail [RoleID] [标题] [内容]
func (r *RoleGM) SendMail(targetID int64, title, content string) error {
    return SendMail(r.ctx, targetID, SendMailOpts{
        Title:   title,
        Content: content,
    })
}

// 用法: send_mail_all [标题] [内容]
func (r *RoleGM) SendMailAll(title, content string) error {
    return SendMailToAll(r.ctx, SendMailOpts{
        Title:   title,
        Content: content,
    })
}

// 用法: send_mail_goods [RoleID] [标题] [内容] [GoodID:Num,...]
func (r *RoleGM) SendMailGoods(targetID int64, title, content, goodsStr string) error {
    var goods []bag.Good
    for _, part := range strings.Split(goodsStr, ",") {
        part = strings.TrimSpace(part)
        if part == "" {
            continue
        }
        kv := strings.Split(part, ":")
        if len(kv) != 2 {
            continue
        }
        gid := gconv.Int(kv[0])
        num := gconv.Int(kv[1])
        goods = append(goods, bag.Good{GoodID: gid, Num: uint64(num)})
    }
    return SendMail(r.ctx, targetID, SendMailOpts{
        Title:       title,
        Content:     content,
        Attachments: goods,
    })
}
```

- [ ] **Step 3: 编译验证**

```bash
go build ./...
```
Expected: 编译成功。

- [ ] **Step 4: 提交**

```bash
git add src/apps/role/internal/logic/role_mail.go src/apps/role/internal/logic/role_mail_api.go src/apps/role/internal/logic/role_gm.go
git commit -m "feat(mail): implement SendMail API and GM commands"
```

---

### Task 6: 测试验证

**Files:**
- Create: `src/apps/role/internal/logic/role_mail_test.go`

- [ ] **Step 1: 写测试文件**

```go
package logic

import (
    "context"
    "testing"
    "time"

    "gserver/core/gxypgx"
    "gserver/src/apps/role/internal/logic/bag"
)
    return context.Background(), roleID, mail
}

func TestMailRedDot(t *testing.T) {
    mail := &RoleMail{}
    mail.mailCache = []MailEntry{
        {ID: 1, IsRead: false},
        {ID: 2, IsRead: true, Attachments: []bag.Good{{GoodID: 100, Num: 1}}, IsClaimed: false},
        {ID: 3, IsRead: true, Attachments: []bag.Good{{GoodID: 200, Num: 1}}, IsClaimed: true},
    }

    unread, unclaimed := mail.calcRedDot()
    if unread != 1 {
        t.Errorf("expected 1 unread, got %d", unread)
    }
    if unclaimed != 1 {
        t.Errorf("expected 1 unclaimed, got %d", unclaimed)
    }
}

func TestMailRedDot_Expired(t *testing.T) {
    mail := &RoleMail{}
    mail.mailCache = []MailEntry{
        {ID: 1, IsRead: false, ExpireAt: time.Now().Unix() - 3600},
        {ID: 2, IsRead: false, Attachments: []bag.Good{{GoodID: 100, Num: 1}}, IsClaimed: false, ExpireAt: time.Now().Unix() - 3600},
    }

    unread, unclaimed := mail.calcRedDot()
    if unread != 0 {
        t.Errorf("expected 0 unread for expired mails, got %d", unread)
    }
    if unclaimed != 0 {
        t.Errorf("expected 0 unclaimed for expired mails, got %d", unclaimed)
    }
}

func TestMailFindMail(t *testing.T) {
    mail := &RoleMail{}
    mail.mailCache = []MailEntry{
        {ID: 1, Title: "first"},
        {ID: 2, Title: "second"},
    }

    m := mail.findMail(2)
    if m == nil || m.Title != "second" {
        t.Errorf("findMail(2) = %v, expected second", m)
    }

    m = mail.findMail(999)
    if m != nil {
        t.Errorf("findMail(999) = %v, expected nil", m)
    }
}
```

- [ ] **Step 2: 运行单元测试**

```bash
go test -gcflags=-l ./src/apps/role/internal/logic/ -run TestMail -v
```
Expected: PASS

- [ ] **Step 3: 全量编译验证**

```bash
go build ./...
```
Expected: 编译成功。

- [ ] **Step 4: 提交**

```bash
git add src/apps/role/internal/logic/role_mail_test.go
git commit -m "test(mail): add mail red dot and find tests"
```

---

### Task 7: 更新 OpenWolf 文件

**Files:**
- Modify: `.wolf/anatomy.md`
- Modify: `.wolf/memory.md`

- [ ] **Step 1: 更新 anatomy.md 追加新文件条目**

在 anatomy.md 追加：

```markdown
## src/apps/role/internal/logic/

- `role_mail.go` — MailEntry, RoleMailMeta, SysMailItem 数据模型；RoleMail 模块（协议处理、红点、生命周期） (~X tok)
- `role_mail_api.go` — SendMail, SendMailBatch, SendMailToAll 发邮件 API (~X tok)
- `role_mail_test.go` — TestMailRedDot, TestMailRedDot_Expired, TestMailFindMail (~X tok)

## protocol/client/

- `mail.proto` — ID: 30001~30099, 邮件系统协议（列表、详情、领取、一键领取、删除、通知） (~X tok)
```

- [ ] **Step 2: 在 memory.md 记录**

```markdown
| 21:00 | feat: 邮件系统 MVP 实现 | role_mail.go, role_mail_api.go, mail.proto, role_gm.go | 实施 | ~X tok |
```

- [ ] **Step 3: 提交**

```bash
git add .wolf/anatomy.md .wolf/memory.md
git commit -m "docs: update OpenWolf files for mail system"
```
