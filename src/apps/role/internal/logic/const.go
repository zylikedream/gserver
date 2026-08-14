package logic

import (
	stderrors "errors"
)

var (
	ErrVersionConflict = stderrors.New("optimistic lock version conflict")
)

var (
	GOLD_ITEM_ID       = 1
	WATER_ITEM_ID      = 3
	PLAYER_EXP_ITEM_ID = 5
)

var (
	ErrFlowerMaxLevel         = stderrors.New("flower already at max level")
	ErrFlowerNeedBreak        = stderrors.New("flower needs breakthrough first")
	ErrFlowerBreakMax         = stderrors.New("flower already at max break stage")
	ErrFlowerBreakLevel       = stderrors.New("flower level not enough for breakthrough")
	ErrFlowerBreakPlayerLevel = stderrors.New("player level not enough for breakthrough")
)

var (
	ErrPlayerLevelNotEnough = stderrors.New("player level not enough")
)

var (
	ErrOrderSlotCooldown        = stderrors.New("order slot is in cooldown")
	ErrOrderNotEnough           = stderrors.New("not enough flower products for order")
	ErrOrderMilestoneClaimed    = stderrors.New("milestone already claimed")
	ErrOrderMilestoneNotReached = stderrors.New("completed count not enough for milestone")
)
