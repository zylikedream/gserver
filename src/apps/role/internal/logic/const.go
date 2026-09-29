package logic

var (
	GOLD_ITEM_ID       = 1
	WATER_ITEM_ID      = 3
	PLAYER_EXP_ITEM_ID = 5
)

var (
	ErrFlowerMaxLevel         = clientRejection("flower already at max level")
	ErrFlowerNeedBreak        = clientRejection("flower needs breakthrough first")
	ErrFlowerBreakMax         = clientRejection("flower already at max break stage")
	ErrFlowerBreakLevel       = clientRejection("flower level not enough for breakthrough")
	ErrFlowerBreakPlayerLevel = clientRejection("player level not enough for breakthrough")
)

var (
	ErrPlayerLevelNotEnough = clientRejection("player level not enough")
)

var (
	ErrOrderSlotCooldown        = clientRejection("order slot is in cooldown")
	ErrOrderNotEnough           = clientRejection("not enough flower products for order")
	ErrOrderMilestoneClaimed    = clientRejection("milestone already claimed")
	ErrOrderMilestoneNotReached = clientRejection("completed count not enough for milestone")
)
