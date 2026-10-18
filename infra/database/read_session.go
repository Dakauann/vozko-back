package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const (
	statementTimedOut = "SQLSTATE 57014"
	JITOff            = "SET LOCAL jit = off"
)

func StatementTimeout(value string) string {
	return "SET LOCAL statement_timeout = '" + value + "'"
}

func ReadSessionSettings(workMem, statementTimeout string) []string {
	return []string{"SET LOCAL work_mem = '" + workMem + "'", JITOff, StatementTimeout(statementTimeout), "SET LOCAL plan_cache_mode = force_custom_plan"}
}

func InReadSession(ctx context.Context, db *gorm.DB, settings []string, fn func(tx *gorm.DB) error) error {
	return inSession(ctx, db, settings, &sql.TxOptions{ReadOnly: true}, fn)
}

func InSession(ctx context.Context, db *gorm.DB, settings []string, fn func(tx *gorm.DB) error) error {
	return inSession(ctx, db, settings, nil, fn)
}

func inSession(ctx context.Context, db *gorm.DB, settings []string, options *sql.TxOptions, fn func(tx *gorm.DB) error) error {
	run := func(tx *gorm.DB) error {
		for _, setting := range settings {
			if err := tx.Exec(setting).Error; err != nil {
				return err
			}
		}
		return fn(tx)
	}
	err := db.WithContext(ctx).Transaction(run, options)
	if err != nil && strings.Contains(err.Error(), statementTimedOut) {
		return fmt.Errorf("%w: %w", context.DeadlineExceeded, err)
	}
	return err
}
