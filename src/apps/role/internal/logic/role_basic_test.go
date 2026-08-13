package logic

import (
	"context"
	"testing"

	"gserver/src/apps/role/internal/logic/bag"
)

var basicCfgInited bool

func initBasicTestConfig(t *testing.T) {
	t.Helper()
	initAllTestConfig(t)
}

func setupTestBasic(t *testing.T) *RoleBasic {
	t.Helper()
	initBasicTestConfig(t)

	main := &RoleMain{}
	basicMod := &RoleBasic{
		RoleModule:     RoleModule{Role: main},
		RoleBasicState: RoleBasicState{Level: 1},
	}
	bagMod := &RoleBag{
		RoleModule:   RoleModule{Role: main},
		RoleBagState: RoleBagState{Goods: make(GoodsMap)},
	}
	main.Basic = basicMod
	main.Bag = bagMod
	return basicMod
}

func TestBasicRefreshLevelByExp_LevelByBagExp(t *testing.T) {
	b := setupTestBasic(t)
	b.Role.Bag.Goods[PLAYER_EXP_ITEM_ID] = bag.BagGood{GoodID: PLAYER_EXP_ITEM_ID, Num: 55}

	oldLevel, newLevel := b.RefreshLevelByExp(context.Background(), 0, 55, "test")

	if oldLevel != 1 || newLevel != 3 {
		t.Fatalf("expected 1 -> 3, got %d -> %d", oldLevel, newLevel)
	}
	if b.Level != 3 {
		t.Fatalf("expected level 3, got %d", b.Level)
	}
}

func TestBasicRefreshLevelByExp_KeepMaxLevelAndRetainOverflowExp(t *testing.T) {
	b := setupTestBasic(t)
	b.Role.Bag.Goods[PLAYER_EXP_ITEM_ID] = bag.BagGood{GoodID: PLAYER_EXP_ITEM_ID, Num: 999999}

	_, newLevel := b.RefreshLevelByExp(context.Background(), 0, 999999, "test")

	if newLevel != 20 {
		t.Fatalf("expected level 20, got %d", newLevel)
	}
	if got := b.Role.Bag.Goods[PLAYER_EXP_ITEM_ID].Num; got != 999999 {
		t.Fatalf("expected exp retained, got %d", got)
	}
}
