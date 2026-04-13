package logic

import (
	"context"

	"gserver/core/gxypgx"

	"github.com/gogf/gf/v2/os/glog"
)

// InitRoleSchema 初始化角色模块的数据库表
func InitRoleSchema(ctx context.Context) {
	pgx := gxypgx.PGX()

	// 1. role_basic 表
	if err := createRoleBasicTable(ctx, pgx); err != nil {
		glog.Fatal(ctx, err)
	}

	// 2. role_bag 表
	if err := createRoleBagTable(ctx, pgx); err != nil {
		glog.Fatal(ctx, err)
	}

	// 3. role_sign 表
	if err := createRoleSignTable(ctx, pgx); err != nil {
		glog.Fatal(ctx, err)
	}

	// 4. role_activity 表
	if err := createRoleActivityTable(ctx, pgx); err != nil {
		glog.Fatal(ctx, err)
	}

	// 5. role_account 表
	if err := createRoleAccountTable(ctx, pgx); err != nil {
		glog.Fatal(ctx, err)
	}

	// 6. role_extra 表
	if err := createRoleExtraTable(ctx, pgx); err != nil {
		glog.Fatal(ctx, err)
	}

	glog.Info(ctx, "[schema] all role tables created successfully")
}

// createRoleBasicTable 创建角色基础信息表
func createRoleBasicTable(ctx context.Context, pgx *gxypgx.PGXApp) error {
	sql := `
	CREATE TABLE IF NOT EXISTS role_basic (
		role_id BIGINT PRIMARY KEY,
		role_name VARCHAR(64) NOT NULL,
		head VARCHAR(128),
		login_tm TIMESTAMPTZ,
		logout_tm TIMESTAMPTZ,
		create_tm TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		vip_lv INT NOT NULL DEFAULT 0,
		update_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`

	if err := pgx.CreateTable(ctx, sql); err != nil {
		return err
	}

	// 创建 update_at 索引
	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_role_basic_update_at
	ON role_basic(update_at)
	`
	return pgx.CreateIndex(ctx, indexSQL)
}

// createRoleBagTable 创建背包物品表
func createRoleBagTable(ctx context.Context, pgx *gxypgx.PGXApp) error {
	sql := `
	CREATE TABLE IF NOT EXISTS role_bag (
		role_id BIGINT PRIMARY KEY,
		items JSONB NOT NULL DEFAULT '{}',
		currencies JSONB NOT NULL DEFAULT '{}',
		grid_use INT NOT NULL DEFAULT 0,
		update_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`

	if err := pgx.CreateTable(ctx, sql); err != nil {
		return err
	}

	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_role_bag_update_at
	ON role_bag(update_at)
	`
	return pgx.CreateIndex(ctx, indexSQL)
}

// createRoleSignTable 创建签到记录表
func createRoleSignTable(ctx context.Context, pgx *gxypgx.PGXApp) error {
	sql := `
	CREATE TABLE IF NOT EXISTS role_sign (
		role_id BIGINT PRIMARY KEY,
		draw_time TIMESTAMPTZ,
		sign_day INT NOT NULL DEFAULT 0,
		accum_draw_stage INT[] NOT NULL DEFAULT '{}',
		draw_day INT NOT NULL DEFAULT 0,
		patch INT NOT NULL DEFAULT 0,
		update_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`

	if err := pgx.CreateTable(ctx, sql); err != nil {
		return err
	}

	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_role_sign_update_at
	ON role_sign(update_at)
	`
	return pgx.CreateIndex(ctx, indexSQL)
}

// createRoleActivityTable 创建活动参与表
func createRoleActivityTable(ctx context.Context, pgx *gxypgx.PGXApp) error {
	sql := `
	CREATE TABLE IF NOT EXISTS role_activity (
		role_id BIGINT PRIMARY KEY,
		activitys JSONB NOT NULL DEFAULT '{}',
		update_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`

	if err := pgx.CreateTable(ctx, sql); err != nil {
		return err
	}

	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_role_activity_update_at
	ON role_activity(update_at)
	`
	return pgx.CreateIndex(ctx, indexSQL)
}

// createRoleAccountTable 创建账号角色映射表
func createRoleAccountTable(ctx context.Context, pgx *gxypgx.PGXApp) error {
	sql := `
	CREATE TABLE IF NOT EXISTS role_account (
		role_id BIGINT PRIMARY KEY,
		account_id BIGINT NOT NULL,
		server_id INT NOT NULL,
		update_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		UNIQUE(account_id, server_id)
	)`

	if err := pgx.CreateTable(ctx, sql); err != nil {
		return err
	}

	// update_at 索引
	indexSQL1 := `
	CREATE INDEX IF NOT EXISTS idx_role_account_update_at
	ON role_account(update_at)
	`
	if err := pgx.CreateIndex(ctx, indexSQL1); err != nil {
		return err
	}

	// account_id + server_id 联合索引（用于按账号查询角色）
	indexSQL2 := `
	CREATE INDEX IF NOT EXISTS idx_role_account_account_server
	ON role_account(account_id, server_id)
	`
	return pgx.CreateIndex(ctx, indexSQL2)
}

// createRoleExtraTable 创建扩展数据表
func createRoleExtraTable(ctx context.Context, pgx *gxypgx.PGXApp) error {
	sql := `
	CREATE TABLE IF NOT EXISTS role_extra (
		role_id BIGINT PRIMARY KEY,
		cron_tm TIMESTAMPTZ,
		update_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`

	if err := pgx.CreateTable(ctx, sql); err != nil {
		return err
	}

	indexSQL := `
	CREATE INDEX IF NOT EXISTS idx_role_extra_update_at
	ON role_extra(update_at)
	`
	return pgx.CreateIndex(ctx, indexSQL)
}
