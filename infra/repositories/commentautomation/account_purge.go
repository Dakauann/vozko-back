package commentautomation_repository

import (
	"context"

	"gorm.io/gorm"

	ca "vozko/domain/commentautomation"
	"vozko/domain/shared"
)

var accountPurgeSQL = []string{
	"DELETE FROM comment_rule_executions WHERE source = ? AND rule_id IN (SELECT id::text FROM comment_rules WHERE source = ? AND account_id = ?)",
	"DELETE FROM comment_private_replies WHERE source = ? AND account_id = ?",
	"DELETE FROM comment_rules WHERE source = ? AND account_id = ?",
}

type accountPurger struct {
	db *gorm.DB
}

func NewAccountPurger(db *gorm.DB) ca.AccountPurger {
	if db == nil {
		panic("comment rules account purge: the database is required")
	}
	return &accountPurger{db: db}
}

func (p *accountPurger) PurgeAccount(ctx context.Context, source shared.EntryType, accountID string) error {
	s := string(source)
	args := [][]any{{s, s, accountID}, {s, accountID}, {s, accountID}}
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, stmt := range accountPurgeSQL {
			if err := tx.Exec(stmt, args[i]...).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
