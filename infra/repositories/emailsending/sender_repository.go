package emailsending_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	es "vozko/domain/emailsending"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type senderRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewSenderRepository(db *gorm.DB) es.SenderRepository {
	return &senderRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (r *senderRepository) take(q *gorm.DB) (*es.Sender, error) {
	var row schema.EmailSender
	err := q.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, es.ErrSenderNotFound
	}
	if err != nil {
		return nil, err
	}
	s := toSender(row)
	return &s, nil
}

func (r *senderRepository) Get(ctx context.Context, workspaceID, id string) (*es.Sender, error) {
	return r.take(r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id))
}

func (r *senderRepository) GetDefault(ctx context.Context, workspaceID string) (*es.Sender, error) {
	return r.take(r.db.WithContext(ctx).Where("workspace_id = ? AND is_default", workspaceID))
}

func (r *senderRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]es.Sender, error) {
	var rows []schema.EmailSender
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]es.Sender, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSender(row))
	}
	return out, nil
}

func (r *senderRepository) clearDefault(tx *gorm.DB, workspaceID, exceptID string) error {
	return tx.Model(&schema.EmailSender{}).
		Where("workspace_id = ? AND is_default AND id <> ?", workspaceID, exceptID).
		Updates(map[string]any{"is_default": false, "updated_at": r.now()}).Error
}

func (r *senderRepository) Create(ctx context.Context, s *es.Sender) error {
	now := r.now()
	s.ID = newID(s.ID)
	s.CreatedAt, s.UpdatedAt = now, now
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if s.IsDefault {
			if err := r.clearDefault(tx, s.WorkspaceID, s.ID); err != nil {
				return err
			}
		}
		return tx.Create(toSenderSchema(s)).Error
	})
	if database.IsUniqueViolation(err) {
		return es.ErrSenderExists
	}
	return err
}

func (r *senderRepository) Update(ctx context.Context, s *es.Sender) error {
	s.UpdatedAt = r.now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if s.IsDefault {
			if err := r.clearDefault(tx, s.WorkspaceID, s.ID); err != nil {
				return err
			}
		}
		return database.AffectedOne(tx.Model(&schema.EmailSender{}).
			Where("workspace_id = ? AND id = ?", s.WorkspaceID, s.ID).
			Updates(map[string]any{
				"domain_id":        s.DomainID,
				"local_part":       s.LocalPart,
				"display_name":     s.DisplayName,
				"reply_forward_to": string(s.ReplyForwardTo),
				"is_default":       s.IsDefault,
				"updated_at":       s.UpdatedAt,
			}), es.ErrSenderNotFound)
	})
	if database.IsUniqueViolation(err) {
		return es.ErrSenderExists
	}
	return err
}

func (r *senderRepository) Delete(ctx context.Context, workspaceID, id string) error {
	return database.AffectedOne(r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).Delete(&schema.EmailSender{}), es.ErrSenderNotFound)
}

func (r *senderRepository) MoveDomain(ctx context.Context, workspaceID, fromDomainID, toDomainID string) error {
	err := r.db.WithContext(ctx).Model(&schema.EmailSender{}).
		Where("workspace_id = ? AND domain_id = ?", workspaceID, fromDomainID).
		Updates(map[string]any{"domain_id": toDomainID, "updated_at": r.now()}).Error
	if database.IsUniqueViolation(err) {
		return es.ErrSenderExists
	}
	return err
}
