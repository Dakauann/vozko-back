package audience_repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	ca "vozko/domain/audience"
)

var errPurgeAccountRequired = errors.New("audience account purge: the source and the account are required")

var accountTables = []string{
	"audience_analyses",
	"audience_authors",
	"audience_batches",
	"audience_backfills",
	"audience_container_settings",
	"audience_rollups",
	"audience_settings",
	"audience_alert_rules",
}

type accountPurger struct {
	db *gorm.DB
}

func NewAccountPurger(db *gorm.DB) ca.AccountPurger {
	if db == nil {
		panic("audience account purge: the database is required")
	}
	return &accountPurger{db: db}
}

func (p *accountPurger) PurgeAccount(ctx context.Context, source ca.Source, accountID string) error {
	if strings.TrimSpace(string(source)) == "" || strings.TrimSpace(accountID) == "" {
		return errPurgeAccountRequired
	}
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, table := range accountTables {
			if err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE source = ? AND account_id = ?", table), string(source), accountID).Error; err != nil {
				return fmt.Errorf("audience account purge: %s: %w", table, err)
			}
		}
		return nil
	})
}
