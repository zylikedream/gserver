package gxypgx

import (
	"context"
)

// CreateTable executes CREATE TABLE statement
func (p *PGXApp) CreateTable(ctx context.Context, sql string) error {
	_, err := p.pool.Exec(ctx, sql)
	return err
}

// CreateIndex executes CREATE INDEX statement
func (p *PGXApp) CreateIndex(ctx context.Context, sql string) error {
	_, err := p.pool.Exec(ctx, sql)
	return err
}

// TableExists checks if a table exists
func (p *PGXApp) TableExists(ctx context.Context, tableName string) (bool, error) {
	var exists bool
	query := `
        SELECT EXISTS (
            SELECT FROM information_schema.tables
            WHERE table_schema = 'public'
            AND table_name = $1
        )
    `
	err := p.pool.QueryRow(ctx, query, tableName).Scan(&exists)
	return exists, err
}

// IndexExists checks if an index exists
func (p *PGXApp) IndexExists(ctx context.Context, indexName string) (bool, error) {
	var exists bool
	query := `
        SELECT EXISTS (
            SELECT FROM pg_indexes
            WHERE schemaname = 'public'
            AND indexname = $1
        )
    `
	err := p.pool.QueryRow(ctx, query, indexName).Scan(&exists)
	return exists, err
}
