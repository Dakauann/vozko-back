package carriercampaign_repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/carriercampaign"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

const recipientBatch = 500

type recipientRepository struct {
	db *gorm.DB
}

func NewRecipientRepository(db *gorm.DB) carriercampaign.RecipientRepository {
	return &recipientRepository{db: db}
}

func validRecipient(status carriercampaign.RecipientStatus, reason carriercampaign.SkipReason) error {
	return errors.Join(valid("recipient status", status), validOrEmpty("skip reason", reason))
}

func toRecipient(row *schema.CarrierCampaignRecipient) (*carriercampaign.Recipient, error) {
	r := &carriercampaign.Recipient{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		CampaignID:  row.CampaignID,
		RunID:       row.RunID,
		LeadID:      row.LeadID,
		Phone:       row.Phone,
		Variant:     row.Variant,
		Status:      carriercampaign.RecipientStatus(row.Status),
		SkipReason:  carriercampaign.SkipReason(row.SkipReason),
		MessageID:   string(row.MessageID),
		ErrorCode:   row.ErrorCode,
		SentAt:      row.SentAt,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if err := validRecipient(r.Status, r.SkipReason); err != nil {
		return nil, fmt.Errorf("carrier campaign recipient %s: %w", row.ID, err)
	}
	return r, nil
}

func (r *recipientRepository) CreateMany(ctx context.Context, recipients []carriercampaign.Recipient) error {
	if len(recipients) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([]schema.CarrierCampaignRecipient, 0, len(recipients))
	for _, rc := range recipients {
		if err := validRecipient(rc.Status, rc.SkipReason); err != nil {
			return err
		}
		rows = append(rows, schema.CarrierCampaignRecipient{
			ID:          newID(rc.ID),
			WorkspaceID: rc.WorkspaceID,
			CampaignID:  rc.CampaignID,
			RunID:       rc.RunID,
			LeadID:      rc.LeadID,
			Phone:       rc.Phone,
			Variant:     rc.Variant,
			Status:      string(rc.Status),
			SkipReason:  string(rc.SkipReason),
			MessageID:   schema.OptionalText(rc.MessageID),
			ErrorCode:   rc.ErrorCode,
			SentAt:      rc.SentAt,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "run_id"}, {Name: "lead_id"}}, DoNothing: true}).
			CreateInBatches(&rows, recipientBatch).Error
	})
}

func (r *recipientRepository) take(query *gorm.DB) (*carriercampaign.Recipient, error) {
	var row schema.CarrierCampaignRecipient
	if err := query.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, carriercampaign.ErrRecipientNotFound
		}
		return nil, err
	}
	return toRecipient(&row)
}

func (r *recipientRepository) FindByID(ctx context.Context, id string) (*carriercampaign.Recipient, error) {
	if !wellFormedID(id) {
		return nil, carriercampaign.ErrRecipientNotFound
	}
	return r.take(r.db.WithContext(ctx).Where("id = ?", id))
}

func (r *recipientRepository) FindByMessageID(ctx context.Context, messageID string) (*carriercampaign.Recipient, error) {
	if !wellFormedID(messageID) {
		return nil, carriercampaign.ErrRecipientNotFound
	}
	return r.take(r.db.WithContext(ctx).Where("message_id = ?", messageID))
}

func (r *recipientRepository) settle(ctx context.Context, id string, fields map[string]any) (bool, error) {
	result := r.db.WithContext(ctx).Model(&schema.CarrierCampaignRecipient{}).
		Where("id = ? AND status = ?", id, carriercampaign.RecipientPending).
		Updates(fields)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		return true, nil
	}
	var n int64
	if err := r.db.WithContext(ctx).Model(&schema.CarrierCampaignRecipient{}).Where("id = ?", id).Count(&n).Error; err != nil {
		return false, err
	}
	if n == 0 {
		return false, carriercampaign.ErrRecipientNotFound
	}
	return false, nil
}

func (r *recipientRepository) MarkSent(ctx context.Context, id, messageID string, at time.Time) (bool, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return false, carriercampaign.ErrMessageIDRequired
	}
	return r.settle(ctx, id, map[string]any{"status": carriercampaign.RecipientSent, "message_id": messageID, "sent_at": at})
}

func (r *recipientRepository) MarkSkipped(ctx context.Context, id string, reason carriercampaign.SkipReason) (bool, error) {
	if !reason.Valid() {
		return false, carriercampaign.ErrSkipReasonInvalid
	}
	return r.settle(ctx, id, map[string]any{"status": carriercampaign.RecipientSkipped, "skip_reason": reason})
}

func (r *recipientRepository) MarkFailed(ctx context.Context, id, errorCode string) (bool, error) {
	errorCode = strings.TrimSpace(errorCode)
	if errorCode == "" {
		return false, carriercampaign.ErrErrorCodeRequired
	}
	return r.settle(ctx, id, map[string]any{"status": carriercampaign.RecipientFailed, "error_code": errorCode})
}

func (r *recipientRepository) PendingIDs(ctx context.Context, runID, after string, limit int) ([]string, error) {
	query := r.db.WithContext(ctx).Model(&schema.CarrierCampaignRecipient{}).
		Where("run_id = ? AND status = ?", runID, carriercampaign.RecipientPending)
	if after != "" {
		query = query.Where("id > ?", after)
	}
	var ids []string
	if err := query.Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *recipientRepository) List(ctx context.Context, in carriercampaign.ListRecipientsInput) (*shared.PaginatedResult[*carriercampaign.Recipient], error) {
	query := r.db.WithContext(ctx).Model(&schema.CarrierCampaignRecipient{}).
		Where("workspace_id = ? AND run_id = ?", in.WorkspaceID, in.RunID)
	if in.Status != "" {
		query = query.Where("status = ?", in.Status)
	}
	if in.SkipReason != "" {
		query = query.Where("skip_reason = ?", in.SkipReason)
	}
	if in.Variant != nil {
		query = query.Where("variant = ?", *in.Variant)
	}
	if search := strings.TrimSpace(in.Search); search != "" {
		query = query.Where(`phone LIKE ? ESCAPE '\'`, sqlexpr.LikeContains(search))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	page := shared.NormalizePagination(in.Pagination)
	var rows []schema.CarrierCampaignRecipient
	if err := query.Order("id ASC").Limit(page.PageSize).Offset(page.Offset()).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*carriercampaign.Recipient, 0, len(rows))
	for i := range rows {
		rc, err := toRecipient(&rows[i])
		if err != nil {
			return nil, err
		}
		items = append(items, rc)
	}
	return shared.NewPaginatedResult(items, page, total), nil
}

type recipientCountRow struct {
	Variant    int
	Status     string
	SkipReason string
	Total      int64
}

func addCount(c *carriercampaign.RecipientCounts, row recipientCountRow) error {
	status, reason := carriercampaign.RecipientStatus(row.Status), carriercampaign.SkipReason(row.SkipReason)
	if err := validRecipient(status, reason); err != nil {
		return err
	}
	if c.Skipped == nil {
		c.Skipped = map[carriercampaign.SkipReason]int64{}
	}
	c.Total += row.Total
	switch status {
	case carriercampaign.RecipientPending:
		c.Pending += row.Total
	case carriercampaign.RecipientSent:
		c.Sent += row.Total
	case carriercampaign.RecipientFailed:
		c.Failed += row.Total
	case carriercampaign.RecipientSkipped:
		c.Skipped[reason] += row.Total
	}
	return nil
}

func (r *recipientRepository) Counts(ctx context.Context, runID string) (carriercampaign.RecipientCounts, error) {
	var rows []recipientCountRow
	if err := r.db.WithContext(ctx).Model(&schema.CarrierCampaignRecipient{}).
		Select("status, skip_reason, count(*) AS total").
		Where("run_id = ?", runID).
		Group("status, skip_reason").
		Scan(&rows).Error; err != nil {
		return carriercampaign.RecipientCounts{}, err
	}
	out := carriercampaign.RecipientCounts{Skipped: map[carriercampaign.SkipReason]int64{}}
	for _, row := range rows {
		if err := addCount(&out, row); err != nil {
			return carriercampaign.RecipientCounts{}, err
		}
	}
	return out, nil
}

func (r *recipientRepository) CountsByVariant(ctx context.Context, runID string) (map[int]carriercampaign.RecipientCounts, error) {
	var rows []recipientCountRow
	if err := r.db.WithContext(ctx).Model(&schema.CarrierCampaignRecipient{}).
		Select("variant, status, skip_reason, count(*) AS total").
		Where("run_id = ?", runID).
		Group("variant, status, skip_reason").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[int]carriercampaign.RecipientCounts{}
	for _, row := range rows {
		c := out[row.Variant]
		if err := addCount(&c, row); err != nil {
			return nil, err
		}
		out[row.Variant] = c
	}
	return out, nil
}
