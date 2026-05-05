package logic

import "github.com/pkg/errors"

var (
	ErrVersionConflict = errors.New("optimistic lock version conflict")
)

var (
	GOLD_ITEM_ID       = 1
	WATER_ITEM_ID      = 3
	PLAYER_EXP_ITEM_ID = 5
)

var (
	ErrFlowerMaxLevel         = errors.New("flower already at max level")
	ErrFlowerNeedBreak        = errors.New("flower needs breakthrough first")
	ErrFlowerBreakMax         = errors.New("flower already at max break stage")
	ErrFlowerBreakLevel       = errors.New("flower level not enough for breakthrough")
	ErrFlowerBreakPlayerLevel = errors.New("player level not enough for breakthrough")
)

var (
	ErrPlayerLevelNotEnough = errors.New("player level not enough")
)

var (
	ErrOrderSlotCooldown        = errors.New("order slot is in cooldown")
	ErrOrderNotEnough           = errors.New("not enough flower products for order")
	ErrOrderMilestoneClaimed    = errors.New("milestone already claimed")
	ErrOrderMilestoneNotReached = errors.New("completed count not enough for milestone")
)
