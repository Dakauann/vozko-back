package emailsending_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/emailcampaign"
	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
)

const inUseSQL = `SELECT
	EXISTS (SELECT 1 FROM email_messages WHERE workspace_id = @ws)
	OR EXISTS (SELECT 1 FROM email_campaigns WHERE workspace_id = @ws AND status IN @keeping)
	OR EXISTS (SELECT 1 FROM email_conversations WHERE workspace_id = @ws)
	OR EXISTS (SELECT 1 FROM email_api_sends WHERE workspace_id = @ws)`

type UnusedAccounts struct {
	db *gorm.DB
}

var _ es.UnusedAccounts = (*UnusedAccounts)(nil)

func NewUnusedAccounts(db *gorm.DB) *UnusedAccounts {
	return &UnusedAccounts{db: db}
}

func (r *UnusedAccounts) ListUnusedCandidates(ctx context.Context, activatedBefore time.Time, afterID string, limit int) ([]es.SenderAccount, error) {
	_, limit = page(0, limit)
	q := r.db.WithContext(ctx).
		Where("unused_warned_at IS NOT NULL OR (created_at <= ? AND status IN ? AND NOT EXISTS (SELECT 1 FROM email_messages m WHERE m.workspace_id = email_sender_accounts.workspace_id))",
			activatedBefore, []string{string(es.AccountActive), string(es.AccountDormant)})
	if afterID != "" {
		q = q.Where("id > ?", afterID)
	}
	var rows []schema.EmailSenderAccount
	if err := q.Order("id").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]es.SenderAccount, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAccount(row))
	}
	return out, nil
}

func (r *UnusedAccounts) InUse(ctx context.Context, workspaceID string) (bool, error) {
	return inUse(r.db.WithContext(ctx), workspaceID)
}

func inUse(db *gorm.DB, workspaceID string) (bool, error) {
	keeping := make([]string, 0, len(emailcampaign.AccountKeepingStatuses()))
	for _, s := range emailcampaign.AccountKeepingStatuses() {
		keeping = append(keeping, string(s))
	}
	var used bool
	err := db.Raw(inUseSQL, map[string]any{"ws": workspaceID, "keeping": keeping}).Scan(&used).Error
	return used, err
}

func (r *UnusedAccounts) Forget(ctx context.Context, workspaceID, accountID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account schema.EmailSenderAccount
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND workspace_id = ?", accountID, workspaceID).Take(&account).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		used, err := inUse(tx, workspaceID)
		if err != nil {
			return err
		}
		if used {
			return es.ErrAccountInUse
		}
		var senders, domains []string
		if err := tx.Model(&schema.EmailSender{}).Where("workspace_id = ?", workspaceID).Pluck("id", &senders).Error; err != nil {
			return err
		}
		if err := tx.Model(&schema.EmailSendingDomain{}).Where("workspace_id = ?", workspaceID).Pluck("id", &domains).Error; err != nil {
			return err
		}
		if len(senders) > 0 {
			if err := tx.Model(&schema.EmailCampaign{}).Where("workspace_id = ? AND sender_id IN ?", workspaceID, senders).Update("sender_id", nil).Error; err != nil {
				return err
			}
			if err := tx.Unscoped().Where("workspace_id = ? AND id IN ?", workspaceID, senders).Delete(&schema.EmailInbox{}).Error; err != nil {
				return err
			}
		}
		if len(domains) > 0 {
			if err := tx.Where("domain_id IN ?", domains).Delete(&schema.EmailDailyUsage{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("workspace_id = ?", workspaceID).Delete(&schema.EmailSender{}).Error; err != nil {
			return err
		}
		if err := tx.Where("workspace_id = ?", workspaceID).Delete(&schema.EmailSendingDomain{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", accountID).Delete(&schema.EmailSenderAccount{}).Error
	})
}
