package emailcampaign

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/emailaddress"
	domain "vozko/domain/emailcampaign"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const recipientInsertBatch = 1000

type RecipientRepository struct {
	db  *gorm.DB
	now func() time.Time
}

func NewRecipientRepository(db *gorm.DB) *RecipientRepository {
	return &RecipientRepository{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (r *RecipientRepository) Replace(ctx context.Context, workspaceID, campaignID string, recipients []domain.Recipient) error {
	now := r.now()
	rows := make([]schema.EmailCampaignRecipient, len(recipients))
	for i, rc := range recipients {
		id := rc.ID
		if id == "" {
			id = uuid.NewString()
		}
		rows[i] = schema.EmailCampaignRecipient{
			ID:          id,
			WorkspaceID: workspaceID,
			CampaignID:  campaignID,
			LeadID:      schema.OptionalText(rc.LeadID),
			Address:     string(rc.Address),
			Variant:     string(rc.Variant),
			Status:      string(rc.Status),
			SkipReason:  string(rc.SkipReason),
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("campaign_id = ?", campaignID).Delete(&schema.EmailCampaignRecipient{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.CreateInBatches(rows, recipientInsertBatch).Error
	})
}

func (r *RecipientRepository) Get(ctx context.Context, id string) (*domain.Recipient, error) {
	if len(database.UUIDArray([]string{id})) == 0 {
		return nil, domain.ErrRecipientNotFound
	}
	var row schema.EmailCampaignRecipient
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrRecipientNotFound
		}
		return nil, err
	}
	rc := recipientOf(row)
	return &rc, nil
}

func recipientOf(row schema.EmailCampaignRecipient) domain.Recipient {
	return domain.Recipient{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		CampaignID:  row.CampaignID,
		LeadID:      string(row.LeadID),
		Address:     emailaddress.Address(row.Address),
		Variant:     domain.Variant(row.Variant),
		Status:      domain.RecipientStatus(row.Status),
		SkipReason:  domain.SkipReason(row.SkipReason),
		MessageID:   string(row.MessageID),
		FailureCode: row.FailureCode,
		SentAt:      utc(row.SentAt),
		CreatedAt:   row.CreatedAt.UTC(),
		UpdatedAt:   row.UpdatedAt.UTC(),
	}
}

func (r *RecipientRepository) settle(ctx context.Context, id string, values map[string]any) (bool, error) {
	values["updated_at"] = r.now()
	result := r.db.WithContext(ctx).Model(&schema.EmailCampaignRecipient{}).
		Where("id = ? AND status = ?", id, string(domain.RecipientPending)).Updates(values)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *RecipientRepository) MarkSent(ctx context.Context, id, messageID string, at time.Time) (bool, error) {
	return r.settle(ctx, id, map[string]any{"status": string(domain.RecipientSent), "message_id": schema.OptionalText(messageID), "sent_at": at.UTC()})
}

const endUnsentSQL = `WITH target AS (
	SELECT id, campaign_id, slot_claimed_at IS NOT NULL AS held FROM email_campaign_recipients
	WHERE id = @id AND status = @pending
	FOR UPDATE
), ended AS (
	UPDATE email_campaign_recipients r
	SET status = @status, skip_reason = @skip, failure_code = @failure, slot_claimed_at = NULL, updated_at = @at
	FROM target WHERE r.id = target.id
	RETURNING target.campaign_id, target.held
), released AS (
	UPDATE email_campaigns c SET used_units = c.used_units - 1
	FROM ended WHERE c.id = ended.campaign_id AND ended.held AND c.used_units > 0
	RETURNING c.id
)
SELECT count(*) FROM ended`

func (r *RecipientRepository) endUnsent(ctx context.Context, id string, status domain.RecipientStatus, reason domain.SkipReason, failure string) (bool, error) {
	if len(database.UUIDArray([]string{id})) == 0 {
		return false, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Raw(endUnsentSQL, map[string]any{
		"id": id, "pending": string(domain.RecipientPending), "status": string(status), "skip": string(reason), "failure": failure, "at": r.now(),
	}).Scan(&n).Error
	return n > 0, err
}

func (r *RecipientRepository) MarkSkipped(ctx context.Context, id string, reason domain.SkipReason) (bool, error) {
	return r.endUnsent(ctx, id, domain.RecipientSkipped, reason, "")
}

func (r *RecipientRepository) MarkFailed(ctx context.Context, id, code string) (bool, error) {
	return r.endUnsent(ctx, id, domain.RecipientFailed, domain.SkipNone, code)
}

func (r *RecipientRepository) Counts(ctx context.Context, campaignID string) (domain.RecipientCounts, error) {
	var rows []struct {
		Status     string
		SkipReason string
		N          int64
	}
	err := r.db.WithContext(ctx).Model(&schema.EmailCampaignRecipient{}).
		Select("status, skip_reason, count(*) AS n").Where("campaign_id = ?", campaignID).
		Group("status, skip_reason").Scan(&rows).Error
	if err != nil {
		return domain.RecipientCounts{}, err
	}
	counts := domain.RecipientCounts{Skipped: map[domain.SkipReason]int64{}}
	for _, row := range rows {
		counts.Total += row.N
		switch domain.RecipientStatus(row.Status) {
		case domain.RecipientPending:
			counts.Pending += row.N
		case domain.RecipientSent:
			counts.Sent += row.N
		case domain.RecipientFailed:
			counts.Failed += row.N
		case domain.RecipientSkipped:
			counts.Skipped[domain.SkipReason(row.SkipReason)] += row.N
		}
	}
	return counts, nil
}

func (r *RecipientRepository) PendingIDs(ctx context.Context, campaignID string, variants []domain.Variant, after string, limit int) ([]string, error) {
	q := r.db.WithContext(ctx).Model(&schema.EmailCampaignRecipient{}).Select("id").
		Where("campaign_id = ? AND status = ?", campaignID, string(domain.RecipientPending))
	if len(variants) > 0 {
		values := make([]string, len(variants))
		for i, v := range variants {
			values[i] = string(v)
		}
		q = q.Where("variant IN ?", values)
	}
	if after = strings.TrimSpace(after); after != "" {
		q = q.Where("id > ?", after)
	}
	var ids []string
	if err := q.Order("id").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

type recipientRowScan struct {
	schema.EmailCampaignRecipient
	MessageStatus  string     `gorm:"column:message_status"`
	DeliveredAt    *time.Time `gorm:"column:delivered_at"`
	FirstOpenAt    *time.Time `gorm:"column:first_open_at"`
	FirstClickAt   *time.Time `gorm:"column:first_click_at"`
	BouncedAt      *time.Time `gorm:"column:bounced_at"`
	BounceKind     string     `gorm:"column:bounce_kind"`
	ComplainedAt   *time.Time `gorm:"column:complained_at"`
	UnsubscribedAt *time.Time `gorm:"column:unsubscribed_at"`
	LeadName       string     `gorm:"column:lead_name"`
}

func (r *RecipientRepository) List(ctx context.Context, workspaceID, campaignID string, f domain.RecipientFilter, offset, limit int) ([]domain.RecipientRow, int64, error) {
	base := r.db.WithContext(ctx).Table("email_campaign_recipients AS r").
		Where("r.workspace_id = ? AND r.campaign_id = ?", workspaceID, campaignID)
	if f.Status != "" {
		base = base.Where("r.status = ?", string(f.Status))
	}
	if f.SkipReason != "" {
		base = base.Where("r.skip_reason = ?", string(f.SkipReason))
	}
	if f.Variant != "" {
		base = base.Where("r.variant = ?", string(f.Variant))
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		base = base.Where("r.address ILIKE ?", "%"+escapeLike(strings.ToLower(search))+"%")
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	limit = min(limit, maxListLimit)
	var rows []recipientRowScan
	err := base.Select("r.*, COALESCE(m.status, '') AS message_status, m.delivered_at, m.first_open_at, m.first_click_at, m.bounced_at," +
		" COALESCE(m.bounce_kind, '') AS bounce_kind, m.complained_at, m.unsubscribed_at, COALESCE(l.name, '') AS lead_name").
		Joins("LEFT JOIN email_messages m ON m.id = r.message_id").
		Joins("LEFT JOIN leads l ON l.id = r.lead_id AND l.workspace_id = r.workspace_id AND l.deleted_at IS NULL").
		Order("r.created_at, r.id").Offset(max(offset, 0)).Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]domain.RecipientRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.RecipientRow{
			Recipient:     recipientOf(row.EmailCampaignRecipient),
			LeadName:      row.LeadName,
			MessageStatus: row.MessageStatus,
			DeliveredAt:   utc(row.DeliveredAt),
			FirstOpenAt:   utc(row.FirstOpenAt),
			FirstClickAt:  utc(row.FirstClickAt),
			BouncedAt:     utc(row.BouncedAt),
			BounceKind:    row.BounceKind,
			ComplainedAt:  utc(row.ComplainedAt),
			Unsubscribed:  utc(row.UnsubscribedAt),
		})
	}
	return out, total, nil
}

var _ domain.RecipientRepository = (*RecipientRepository)(nil)

const pendingThroughSQL = "SELECT count(*) FROM email_campaign_recipients WHERE campaign_id = ? AND status = ? AND id <= ?"

func (r *RecipientRepository) PendingThrough(ctx context.Context, campaignID, lastID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Raw(pendingThroughSQL, campaignID, string(domain.RecipientPending), lastID).Scan(&n).Error
	return n, err
}
