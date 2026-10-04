package webchat_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	wcdomain "vozko/domain/webchat"
	"vozko/infra/database/schema"
)

type visitorRepository struct {
	db *gorm.DB
}

func NewVisitorRepository(db *gorm.DB) wcdomain.VisitorRepository {
	return &visitorRepository{db: db}
}

func (r *visitorRepository) Create(ctx context.Context, v *wcdomain.Visitor) error {
	record := toVisitorSchema(v)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	v.ID = record.ID
	v.CreatedAt = record.CreatedAt
	v.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *visitorRepository) FindByID(ctx context.Context, id string) (*wcdomain.Visitor, error) {
	var record schema.WebchatVisitor
	if err := r.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wcdomain.ErrVisitorNotFound
		}
		return nil, err
	}
	return toVisitorDomain(&record), nil
}

func (r *visitorRepository) FindByIDs(ctx context.Context, ids []string) ([]*wcdomain.Visitor, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var records []schema.WebchatVisitor
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*wcdomain.Visitor, 0, len(records))
	for i := range records {
		out = append(out, toVisitorDomain(&records[i]))
	}
	return out, nil
}

func (r *visitorRepository) FindByExternalID(ctx context.Context, widgetID, externalID string) (*wcdomain.Visitor, error) {
	var record schema.WebchatVisitor
	if err := r.db.WithContext(ctx).
		First(&record, "widget_id = ? AND external_id = ?", widgetID, externalID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wcdomain.ErrVisitorNotFound
		}
		return nil, err
	}
	return toVisitorDomain(&record), nil
}

func (r *visitorRepository) ApplyIdentity(ctx context.Context, id string, claims wcdomain.IdentityClaims) error {
	update := map[string]any{"identity_verified": true}
	if claims.Name != "" {
		update["name"] = claims.Name
	}
	if claims.Email != "" {
		update["email"] = claims.Email
	}
	if claims.Phone != "" {
		update["phone"] = claims.Phone
	}
	return r.updates(ctx, id, update)
}

func (r *visitorRepository) SaveIntake(ctx context.Context, id string, intake wcdomain.Intake, leadID *string, at time.Time) error {
	update := map[string]any{"intake_completed_at": at}
	if intake.Name != "" {
		update["name"] = intake.Name
	}
	if intake.Email != "" {
		update["email"] = intake.Email
	}
	if intake.Phone != "" {
		update["phone"] = intake.Phone
	}
	if intake.ConsentedAt != nil {
		update["consented_at"] = intake.ConsentedAt
	}
	if leadID != nil {
		update["lead_id"] = leadID
	}
	return r.updates(ctx, id, update)
}

func (r *visitorRepository) Touch(ctx context.Context, id string, seen wcdomain.VisitorSighting) error {
	update := map[string]any{"last_seen_at": seen.At}
	if seen.IPHash != "" {
		update["ip_hash"] = seen.IPHash
	}
	if seen.UserAgent != "" {
		update["user_agent"] = seen.UserAgent
	}
	if seen.Locale != "" {
		update["locale"] = seen.Locale
	}
	if seen.PageOrigin != "" {
		update["page_origin"] = seen.PageOrigin
	}
	return r.updates(ctx, id, update)
}

func (r *visitorRepository) SetBlocked(ctx context.Context, id string, blocked bool, at time.Time) error {
	var blockedAt *time.Time
	if blocked {
		blockedAt = &at
	}
	return r.updates(ctx, id, map[string]any{"blocked": blocked, "blocked_at": blockedAt})
}

func (r *visitorRepository) updates(ctx context.Context, id string, update map[string]any) error {
	result := r.db.WithContext(ctx).Model(&schema.WebchatVisitor{}).Where("id = ?", id).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wcdomain.ErrVisitorNotFound
	}
	return nil
}

func toVisitorSchema(v *wcdomain.Visitor) *schema.WebchatVisitor {
	return &schema.WebchatVisitor{
		ID:                v.ID,
		WorkspaceID:       v.WorkspaceID,
		WidgetID:          v.WidgetID,
		ExternalID:        v.ExternalID,
		Name:              v.Name,
		Email:             v.Email,
		Phone:             v.Phone,
		LeadID:            v.LeadID,
		IdentityVerified:  v.IdentityVerified,
		IntakeCompletedAt: v.IntakeCompletedAt,
		ConsentedAt:       v.ConsentedAt,
		Locale:            v.Locale,
		UserAgent:         v.UserAgent,
		IPHash:            v.IPHash,
		PageOrigin:        v.PageOrigin,
		LastSeenAt:        v.LastSeenAt,
		Blocked:           v.Blocked,
		BlockedAt:         v.BlockedAt,
	}
}

func toVisitorDomain(record *schema.WebchatVisitor) *wcdomain.Visitor {
	return &wcdomain.Visitor{
		ID:                record.ID,
		WorkspaceID:       record.WorkspaceID,
		WidgetID:          record.WidgetID,
		ExternalID:        record.ExternalID,
		Name:              record.Name,
		Email:             record.Email,
		Phone:             record.Phone,
		LeadID:            record.LeadID,
		IdentityVerified:  record.IdentityVerified,
		IntakeCompletedAt: record.IntakeCompletedAt,
		ConsentedAt:       record.ConsentedAt,
		Locale:            record.Locale,
		UserAgent:         record.UserAgent,
		IPHash:            record.IPHash,
		PageOrigin:        record.PageOrigin,
		LastSeenAt:        record.LastSeenAt,
		Blocked:           record.Blocked,
		BlockedAt:         record.BlockedAt,
		CreatedAt:         record.CreatedAt,
		UpdatedAt:         record.UpdatedAt,
	}
}
