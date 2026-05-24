# 错误案例记录

> 犯过的错误，原因和解决方案。新增案例直接在末尾追加。

---

## Case 1: RoleMail.OnModInit 冗余调用 loadModuleState

**时间：** 2026-05-21

**现象：**

RoleMail 的 OnModInit 中手动调了 `loadModuleState` 和 `SetRoleID`，而其他模块（Bag、Flower 等）没有这两个调用。

```go
func (r *RoleMail) OnModInit(ctx context.Context) error {
    roleID := r.RoleID
    if err := loadModuleState(ctx, roleID, &r.meta); err != nil { // 多余
        return err
    }
    r.meta.SetRoleID(roleID) // 多余
    ...
}
```

**原因：**

`role_main.go:loadModules` 在 OnModInit 之前已经统一做了两件事：
1. 遍历所有模块调 `PersistState()` → `loadModuleState()` 从 DB 加载状态
2. 遍历所有模块调 `SetRole(r)` 注入 RoleID 和 Role 指针

写 RoleMail 之前没有参考已有模块（Bag、Flower、Plot）的 OnModInit，凭直觉认为"初始化需要加载状态"就加上了。如果看一眼 Bag 的 `OnModInit`（直接 `return nil`），不会犯这个错。

**解决方案：**

删掉 `loadModuleState` 和 `SetRoleID` 两行，OnModInit 从业务初始化（加载邮件列表）开始。`go build ./...` 通过，行为无变化。

**教训：**

- 新增模块前先看一个已有模块的对应实现，确认模式后再写
- `loadModules` 统一做了状态加载和 Role 注入，各 OnModInit 不需要重复
