package gxypgx

import (
	"context"
	"fmt"
	"strings"
)

// InsertOne inserts a single row
func (p *PGXApp) InsertOne(ctx context.Context, tableName string, columns []string, values []interface{}) error {
	query := p.buildInsertQuery(tableName, columns)
	_, err := p.pool.Exec(ctx, query, values...)
	return err
}

// FindOne queries a single row
func (p *PGXApp) FindOne(ctx context.Context, tableName string, dest interface{}, where string, args ...interface{}) error {
	query := "SELECT * FROM " + tableName + " WHERE " + where
	row := p.pool.QueryRow(ctx, query, args...)
	return row.Scan(dest)
}

// Update updates data
func (p *PGXApp) Update(ctx context.Context, tableName string, set string, where string, args ...interface{}) error {
	query := "UPDATE " + tableName + " SET " + set + " WHERE " + where
	_, err := p.pool.Exec(ctx, query, args...)
	return err
}

// Delete deletes data
func (p *PGXApp) Delete(ctx context.Context, tableName string, where string, args ...interface{}) error {
	query := "DELETE FROM " + tableName + " WHERE " + where
	_, err := p.pool.Exec(ctx, query, args...)
	return err
}

// buildInsertQuery builds INSERT statement (placeholders format $1, $2, ...)
func (p *PGXApp) buildInsertQuery(tableName string, columns []string) string {
	var query strings.Builder
	placeholderCount := len(columns)

	query.WriteString("INSERT INTO ")
	query.WriteString(tableName)
	query.WriteString(" (")

	for i, col := range columns {
		if i > 0 {
			query.WriteString(", ")
		}
		query.WriteString(col)
	}

	query.WriteString(") VALUES (")

	for i := 0; i < placeholderCount; i++ {
		if i > 0 {
			query.WriteString(", ")
		}
		// pgx uses $1, $2, ... format (1-indexed)
		query.WriteString(fmt.Sprintf("$%d", i+1))
	}

	query.WriteString(")")
	return query.String()
}
