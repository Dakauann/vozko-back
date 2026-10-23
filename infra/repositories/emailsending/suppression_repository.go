package emailsending_repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/emailaddress"
	"vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
	emailhygiene_repository "vozko/infra/repositories/emailhygiene"
)

type suppressionRepository struct {
	db       *gorm.DB
	standing emailhygiene.StandingReader
	now      func() time.Time
}

func NewSuppressionRepository(db *gorm.DB) es.SuppressionRepository {
	return &suppressionRepository{db: db, standing: emailhygiene_repository.NewStanding(db), now: func() time.Time { return time.Now().UTC() }}
}

func (r *suppressionRepository) Suppress(ctx context.Context, s es.Suppression) error {
	now := r.now()
	at := s.SuppressedAt
	if at.IsZero() {
		at = now
	}
	row := schema.EmailSuppression{
		ID:              newID(""),
		WorkspaceID:     s.WorkspaceID,
		Address:         string(s.Address),
		Reason:          string(s.Reason),
		SourceMessageID: s.SourceMessage,
		SuppressedAt:    at,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "address"}},
		DoNothing: true,
	}).Create(&row).Error
}

func (r *suppressionRepository) Remove(ctx context.Context, workspaceID string, address emailaddress.Address) error {
	return r.db.WithContext(ctx).Where("workspace_id = ? AND address = ?", workspaceID, string(address)).Delete(&schema.EmailSuppression{}).Error
}

func (r *suppressionRepository) Suppressed(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]es.SuppressReason, error) {
	out := map[emailaddress.Address]es.SuppressReason{}
	standing, err := r.standing.Standing(ctx, workspaceID, addresses)
	if err != nil {
		return nil, err
	}
	for address, s := range standing {
		if reason := s.Refusal(); reason != "" {
			out[address] = reason
		}
	}
	return out, nil
}

func (r *suppressionRepository) List(ctx context.Context, workspaceID string, query es.SuppressionQuery) ([]es.Suppression, int64, error) {
	offset, limit := page(query.Offset, query.Limit)
	scope := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&schema.EmailSuppression{}).Where("workspace_id = ?", workspaceID)
		if query.Prefix != "" {
			q = q.Where(`address LIKE ? ESCAPE '\'`, sqlexpr.LikePrefix(query.Prefix))
		}
		return q
	}
	var total int64
	if err := scope().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []schema.EmailSuppression
	if err := scope().Order("suppressed_at DESC, id").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]es.Suppression, 0, len(rows))
	for _, row := range rows {
		out = append(out, es.Suppression{
			WorkspaceID:   row.WorkspaceID,
			Address:       emailaddress.Address(row.Address),
			Reason:        es.SuppressReason(row.Reason),
			SourceMessage: row.SourceMessageID,
			SuppressedAt:  row.SuppressedAt,
		})
	}
	return out, total, nil
}
