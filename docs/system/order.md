# 居民订单系统

## 定位

花产品消耗出口、循环售卖玩法、前期经济来源。

## 架构

```
订单点位(5个) --绑定--> 订单模板(新手/普通/进阶)
                              |
                    居民随机池 + KindProbs + NeedMin/Max + Reward
                              |
                    刷新时从已培育花中随机选花产品
```

## 数据流

- **OnCreate / OnModInit**: 遍历所有点位配置，为每个 slot 生成第一张订单
- **ReqOrderInfo** (26001): 返回所有 slot 当前订单 + 累计完成数 + 里程碑状态
- **ReqSubmitOrder** (26003): 校验冷却 → 扣花产品 → 发奖励 → 生成下一张订单 → 设冷却 → 累计完成数++ → 发布事件
- **ReqClaimOrderMilestone** (26005): 校验完成数 → 校验未领取 → 发奖励

## 刷新时机

提交订单时 **立即生成下一张订单**，带上 `cooldown_end`。客户端根据 `cooldown_end > now` 判断是否可提交，做倒计时显示。无服务端定时器或查询时刷新。

## Slot 状态

Slot 始终有订单数据（首个订单在创建时生成）。`cooldown_end` 控制可提交时机，无独立 State 枚举。

## 订单生成规则

1. 从模板 ResidentIds 随机居民
2. 从 FLOWER_HARVESTED 鲜花中收集可用花产品（通过 TbFlower.HarvestItemId 映射）
3. 按可用花产品数过滤 KindProbs（只有 1 种花时移除 2 种需求概率）
4. 按过滤后的 KindProbs 加权随机需求种类数
5. 从可用花产品中随机 N 种，每种数量从 [NeedMin, NeedMax] 区间随机

## 配置表

| 表 | 文件 | 说明 |
|----|------|------|
| 居民 | `gameconfig/json/garden_tbresident.json` | 居民形象和文案 |
| 订单点位 | `gameconfig/json/garden_tbresidentorderslot.json` | 5 个点位位置、冷却、绑定模板 |
| 订单模板 | `gameconfig/json/garden_tbresidentorder.json` | 居民池、需求概率、数量范围、奖励 |
| 累计奖励 | `gameconfig/json/garden_tbresidentorderprogressreward.json` | 15/30/45/60 节点奖励 |

## GM 命令

```
add_order_flower [花ID]  # 设置鲜花为已培育完成状态
```

## Proto

- ID 段: 26001~26006
- 文件: `protocol/client/order.proto`
- 复用 `bag.proto` 的 `PGoodInfo` 结构

## 事件

`EVENT_ORDER_COMPLETE` — 订单完成时发布，数据包含 SlotID。
