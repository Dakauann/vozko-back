package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const statementTimedOut = "SQLSTATE 57014"

func ReadSessionSettings(workMem, statementTimeout string) []string {
	return []string{"SET LOCAL work_mem = '" + workMem + "'", "SET LOCAL jit = off", "SET LOCAL statement_timeout = '" + statementTimeout + "'"}
}

func InReadSession(ctx context.Context, db *gorm.DB, settings []string, fn func(tx *gorm.DB) error) error {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, setting := range settings {
			if err := tx.Exec(setting).Error; err != nil {
				return err
			}
		}
		return fn(tx)
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil && strings.Contains(err.Error(), statementTimedOut) {
		return fmt.Errorf("%w: %w", context.DeadlineExceeded, err)
	}
	return err
}
