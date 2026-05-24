# 邮件系统设计

> 日期：2026-05-21  
> 范围：邮件系统首版  
> 定位：系统通知、补偿发放、奖励领取

## 1. 架构

邮件系统采用 **Role 模块独立行表** 模式：

- **RoleMail** — 角色模块，处理玩家邮件操作的协议（列表、详情、领取、删除、红点）
- **RoleMailMeta** — 角色模块元数据（自增ID计数器、全服邮件展开位置）
- **role_mail_items** — 独立行表，每封邮件一行，外部系统直接 INSERT 发信
- **sys_mail** — 全服邮件表，展开后才进入玩家的 role_mail_items
- **SendMail API** — Go 内部接口供 GM/系统逻辑调用来发邮件

### 与其他系统的关系

```
GM 命令 → SendMail() → INSERT role_mail_items
系统逻辑 → SendMail() → INSERT role_mail_items
全服邮件 → SendMailToAll() → INSERT sys_mail → 玩家登录时展开
                         ↓
                  PublishRoleNotify（在线玩家刷新红点）
```

## 2. 数据模型

### 2.1 邮件条目（role_mail_items）

每封邮件一行，独立存储。

```sql
CREATE TABLE role_mail_items (
    id           BIGSERIAL PRIMARY KEY,
    role_id      BIGINT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',
    attachments  JSONB NOT NULL DEFAULT '[]',
    send_at      BIGINT NOT NULL DEFAULT 0,
    expire_at    BIGINT NOT NULL DEFAULT 0,
    is_read      BOOLEAN NOT NULL DEFAULT FALSE,
    is_claimed   BOOLEAN NOT NULL DEFAULT FALSE,
    is_sys_mail  BOOLEAN NOT NULL DEFAULT FALSE,  -- 是否来自全服邮件
    sys_mail_id  BIGINT NOT NULL DEFAULT 0,       -- 对应的全服邮件ID
    created_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_role_mail_role_id ON role_mail_items (role_id);
```

Go 结构：

```go
type MailEntry struct {
    ID          int64      `gorm:"primaryKey"`
    RoleID      int64      `gorm:"column:role_id"`
    Title       string     `gorm:"column:title"`
    Summary     string     `gorm:"column:summary"`
    Content     string     `gorm:"column:content"`
    Attachments []bag.Good `gorm:"column:attachments;type:jsonb;serializer:json"`
    SendAt      int64      `gorm:"column:send_at"`
    ExpireAt    int64      `gorm:"column:expire_at"`
    IsRead      bool       `gorm:"column:is_read"`
    IsClaimed   bool       `gorm:"column:is_claimed"`
    IsSysMail   bool       `gorm:"column:is_sys_mail"`
}

func (MailEntry) TableName() string { return "role_mail_items" }
```

### 2.2 角色邮件元数据（role_mail_meta）

```go
type RoleMailMeta struct {
    RolePersistState
    MaxID             int64 `gorm:"column:max_id"`
    LastExpandSysMail int64 `gorm:"column:last_expand_sys_mail_id"`
}

func (RoleMailMeta) TableName() string { return "role_mail_meta" }
```

### 2.3 全服邮件（sys_mail）

```go
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

### 2.4 配置表

mailconfig 表（追加到 gameconfig）：

| 字段 | 含义 | 默认值 |
|------|------|--------|
| MailboxMaxCount | 邮箱容量上限 | 100 |
| DefaultExpireDays | 默认过期天数 | 30 |
| ClaimLimitPerClick | 一键领取数量上限 | 100 |
| TitleMaxLen | 标题字数上限 | 50 |
| ContentMaxLen | 正文字数上限 | 1000 |

## 3. 协议

`protocol/client/mail.proto`，ID 30001~30099。

### 邮件列表（30001）

```proto
message ReqMailList {}
message RspMailList {
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
```

### 邮件详情（30002）

```proto
message ReqMailDetail { int64 mail_id = 1; }
message RspMailDetail { PMailDetail mail = 1; }
message PMailDetail {
    int64 id = 1;
    string title = 2;
    string content = 3;
    int64 send_at = 4;
    int64 expire_at = 5;
    repeated PGoodInfo attachments = 6;
    bool is_claimed = 7;
}
```

### 领取附件（30003）

```proto
message ReqMailClaim { int64 mail_id = 1; }
message RspMailClaim {
    repeated PGoodInfo rewards = 1;
    int32 unclaimed_count = 2;
}
```

### 一键领取（30004）

```proto
message ReqMailClaimAll {}
message RspMailClaimAll {
    repeated PGoodInfo rewards = 1;
    int32 unclaimed_count = 2;
}
```

### 删除邮件（30005）

```proto
message ReqMailDelete { int64 mail_id = 1; }
message RspMailDelete {
    int32 unread_count = 2;
    int32 unclaimed_count = 3;
}
```

### 新邮件通知（推送）

```proto
message NotifyMailUpdate {
    int32 unread_count = 1;
    int32 unclaimed_count = 2;
}
```

## 4. RoleMail 模块

### 模块结构

```go
type RoleMail struct {
    RoleModule
    mailCache []MailEntry
    meta      RoleMailMeta
}
```

### 生命周期

**OnModInit:**
1. `SELECT * FROM role_mail_items WHERE role_id=? AND is_deleted=false ORDER BY id DESC`
2. 全服邮件展开：检查 `LastExpandSysMail < max(sys_mail.id)`
   - 有未展开的全服邮件 → 逐条 INSERT role_mail_items → 追加到 mailCache
3. 过期清理：标记过期邮件 `is_deleted=true`，从 cache 移除

**AfterLogin:**
- 重新展开全服邮件（检查新的全服邮件）

### 协议处理

| 协议 | 逻辑 |
|------|------|
| ReqMailList | 返回 mailCache 过滤过期/已删，附带红点计数 |
| ReqMailDetail | 按 ID 查询 cache → 标记 IsRead（UPDATE DB + mark dirty） |
| ReqMailClaim | 校验未过期未领取 → Bag.SaveGoods 发放 → 标记 IsClaimed |
| ReqMailClaimAll | 遍历 cache 过滤可领取 → SaveGoods 汇总发放 |
| ReqMailDelete | 校验无附件或已领取 → 软删除（UPDATE is_deleted=true） |

### 红点计算

```go
func (r *RoleMail) calcRedDot() (unread, unclaimed int32) {
    for _, m := range r.mailCache {
        if m.ExpireAt > 0 && m.ExpireAt < now.Unix() {
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

红点状态在每次 Rsp 里附带，客户端无需额外查询。

## 5. 发邮件 API

### 核心接口

```go
// 给单个玩家发邮件（外部系统调用，直接 INSERT DB）
func SendMail(ctx context.Context, roleID int64, opts SendMailOpts) error

// 给多个玩家发邮件（批量补偿等）
func SendMailBatch(ctx context.Context, roleIDs []int64, opts SendMailOpts) error

// 发全服邮件（写入 sys_mail，玩家登录时展开）
func SendMailToAll(ctx context.Context, opts SendMailOpts) error

type SendMailOpts struct {
    Title       string
    Summary     string
    Content     string
    Attachments []bag.Good
    ExpireAt    int64     // 0 表示使用默认过期天数
    IsImportant bool      // 后续扩展
}
```

### 通知在线玩家

```go
func notifyMailUpdate(ctx context.Context, roleID int64) {
    // 发给 role actor，不强制激活
    rolelib.PublishRoleNotify(ctx, roleID, &pb.NotifyMailUpdate{
        // 在线玩家收到后刷新红点
    })
}
```

### GM 命令

```go
// 用法: send_mail [RoleID] [标题] [内容]
// 用法: send_mail_all [标题] [内容]
// 用法: send_mail_goods [RoleID] [标题] [内容] [GoodID:Num,...]
```

## 6. 全服邮件展开流程

```
发送端：
  1. INSERT sys_mail (title, content, attachments, expire_at)

接收端（玩家 OnModInit / AfterLogin）：
  1. SELECT MAX(id) FROM sys_mail → 拿到当前最大ID
  2. if meta.LastExpandSysMail < maxID:
  3.   SELECT * FROM sys_mail WHERE id > LastExpandSysMail
  4.   for each sysMail:
  5.     INSERT INTO role_mail_items (role_id, title, content, ..., is_sys_mail=true, sys_mail_id=sysMail.id)
  6.     mailCache = append(mailCache, newMail)
  7.   meta.LastExpandSysMail = maxID
```

## 7. 过期与容量管理

### 过期清理

- **被动清理**：OnModInit 时，对 `expire_at < now` 的邮件标记软删除
- **定时清理**：可选的 TickSave 中对过期邮件做批量 UPDATE

### 容量管理

- OnModInit 检查 mailCache 数量，超过 `MailboxMaxCount` 时：
  1. 优先删除已读、无附件、已领取、过期的陈旧邮件
  2. 不自动删除有未领取附件的邮件
- 新邮件到达时如果已满，由 SendMail 策略决定如何处理（默认：丢弃并记录日志）

## 8. 首版不做

- 玩家间互发邮件
- 邮件回复/转发
- 附件退回
- 邮件分类页签
- 批量删除
- 附件过期前提醒
- 重要邮件置顶
- 排序优化（首版按时间倒序）
