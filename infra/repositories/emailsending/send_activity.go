package emailsending_repository

import (
	"context"
	"database/sql"
	"time"

	"gorm.io/gorm"

	es "vozko/domain/emailsending"
)

const (
	lastSentSQL       = `SELECT max(sent_at) FROM email_messages WHERE workspace_id = ? AND sent_at IS NOT NULL`
	sentFromSharedSQL = `SELECT EXISTS (SELECT 1 FROM email_messages m JOIN email_sending_domains d ON d.id = m.domain_id WHERE m.workspace_id = ? AND d.workspace_id = ? AND d.kind IN ?)`
)

type sendActivity struct {
	db *gorm.DB
}

func NewSendActivity(db *gorm.DB) es.SendActivity {
	return &sendActivity{db: db}
}

func (r *sendActivity) LastSentAt(ctx context.Context, workspaceID string) (*time.Time, error) {
	var last sql.NullTime
	if err := r.db.WithContext(ctx).Raw(lastSentSQL, workspaceID).Scan(&last).Error; err != nil {
		return nil, err
	}
	if !last.Valid {
		return nil, nil
	}
	at := last.Time.UTC()
	return &at, nil
}

func (r *sendActivity) SentFromSharedDomains(ctx context.Context, workspaceID string) (bool, error) {
	var sent bool
	kinds := []string{string(es.DomainSharedProbation), string(es.DomainSharedTrusted)}
	if err := r.db.WithContext(ctx).Raw(sentFromSharedSQL, workspaceID, workspaceID, kinds).Scan(&sent).Error; err != nil {
		return false, err
	}
	return sent, nil
}
