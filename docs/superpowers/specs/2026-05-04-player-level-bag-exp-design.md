# Player Level Bag Experience Design

## Context

The player level system is a pure progression unlock system. The updated design treats player experience as a special currency item instead of a server-side auto-use item. This keeps reward delivery, reward display, and stored player progress on the same path.

Experience item `5` is stored in `RoleBag.Goods` like other currencies. The client hides it from the normal bag view based on item configuration, but can still show it in reward popups and use it to calculate level progress from the level table.

## Goals

- Store cumulative player experience in the bag as item `5`.
- Persist `RoleBasic.Level` as the server-side current level cache.
- Remove server-side `auto_use_on_gain` handling from the bag flow.
- Let normal reward notifications include experience changes.
- Keep level gates for plot unlock and flower breakthrough server-authoritative.
- Allow experience to continue accumulating after the current configured max level.

## Non-Goals

- Do not add `role_basic.total_exp`.
- Do not send `next_level_exp` from the server.
- Do not implement automatic server-side chest opening.
- Do not add separate reward protocol fields for experience.

## Data Model

`RoleBag.Goods[PLAYER_EXP_ITEM_ID]` is the source of truth for cumulative experience. Missing item `5` means zero experience.

`RoleBasic.Level` remains persisted with a default value of `1`. On module initialization and after experience changes, the server refreshes this field from the bag experience value and `TbPlayerLevel`.

The level calculation selects the highest configured player level whose `total_exp` is less than or equal to the stored experience. If stored experience exceeds the current configured max level, the level stays at the max configured level. The experience value is not capped, so future level table expansion can immediately promote eligible players.

## Bag Flow

`SaveGoods` performs only generic atomic remove/add operations. It no longer splits auto-use items or dispatches item use behavior.

After successful add operations, if the added goods include item `5`, `RoleBasic.RefreshLevelByExp(ctx, reason)` runs before notifications and persistence finalize. This lets level-up notifications be sent in the same reward flow while `NotifyBagUpdate` still includes the experience item change.

Future chest items should use an explicit client request such as a use-item protocol. The server then validates and converts chest contents through normal `SaveGoods` calls.

## Protocol

`RspBasicInfo` includes `level` only for the level system. The client obtains current experience from bag/currency data and calculates next-level progress from synchronized config.

`NotifyRoleLevelUp` includes `old_level`, `new_level`, and `unlock_desc`. It does not include total experience or next-level experience.

## Error Handling

If player level config is missing, refresh keeps level at `1` and does not block login. If the bag module is unavailable during refresh, missing experience is treated as zero.

Existing database rows must load safely after migration. `RoleBasic.Level` should have a default of `1`; no nullable numeric `total_exp` column is introduced.

## Tests

Update role logic tests to use real game config tables.

Cover these cases:

- Adding experience item `5` stores it in `RoleBag.Goods`.
- Adding experience refreshes `RoleBasic.Level`.
- Adding normal items and experience together produces normal bag updates for both.
- Experience above the configured max level is retained, while level stays at the configured max.
- Plot unlock and flower breakthrough continue to check `RoleBasic.Level`.
- Remove old auto-use tests and constants that no longer apply.
