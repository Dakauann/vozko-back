package advertising_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

type leadFormRepository struct {
	db *gorm.DB
}

func NewLeadFormRepository(db *gorm.DB) advertising.LeadFormRepository {
	return &leadFormRepository{db: db}
}

const trackLeadFormSQL = `INSERT INTO ad_lead_forms (meta_id, workspace_id, ad_account_id, page_id, name) VALUES (?, ?, ?, ?, ?)
ON CONFLICT (meta_id) DO UPDATE SET
	ad_account_id = EXCLUDED.ad_account_id,
	page_id = EXCLUDED.page_id,
	name = EXCLUDED.name
WHERE ad_lead_forms.workspace_id = EXCLUDED.workspace_id`

func (r *leadFormRepository) Track(ctx context.Context, f *advertising.TrackedForm) error {
	if blank(f.WorkspaceID) {
		return advertising.ErrWorkspaceRequired
	}
	if blank(f.MetaID) {
		return errMetaIDRequired
	}
	result := r.db.WithContext(ctx).Exec(trackLeadFormSQL, f.MetaID, f.WorkspaceID, f.AdAccountID, f.PageID, f.Name)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errFormTrackedElsewhere
	}
	return nil
}

func (r *leadFormRepository) FindByMetaID(ctx context.Context, metaID string) (*advertising.TrackedForm, error) {
	var record schema.AdLeadForm
	if err := r.db.WithContext(ctx).First(&record, "meta_id = ?", metaID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, advertising.ErrLeadFormNotFound
		}
		return nil, err
	}
	return toTrackedForm(&record), nil
}

func (r *leadFormRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]*advertising.TrackedForm, error) {
	if blank(workspaceID) {
		return nil, advertising.ErrWorkspaceRequired
	}
	var records []schema.AdLeadForm
	if err := r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("name, meta_id").Find(&records).Error; err != nil {
		return nil, err
	}
	return toTrackedForms(records), nil
}

func (r *leadFormRepository) ListAll(ctx context.Context, limit, offset int) ([]*advertising.TrackedForm, error) {
	var records []schema.AdLeadForm
	if err := r.db.WithContext(ctx).Order("last_polled_at ASC NULLS FIRST, meta_id").
		Limit(limit).Offset(offset).Find(&records).Error; err != nil {
		return nil, err
	}
	return toTrackedForms(records), nil
}

func (r *leadFormRepository) MarkPolled(ctx context.Context, metaID string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&schema.AdLeadForm{}).Where("meta_id = ?", metaID).Update("last_polled_at", at)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return advertising.ErrLeadFormNotFound
	}
	return nil
}

func toTrackedForms(records []schema.AdLeadForm) []*advertising.TrackedForm {
	out := make([]*advertising.TrackedForm, 0, len(records))
	for i := range records {
		out = append(out, toTrackedForm(&records[i]))
	}
	return out
}

func toTrackedForm(record *schema.AdLeadForm) *advertising.TrackedForm {
	return &advertising.TrackedForm{
		MetaID:       record.MetaID,
		WorkspaceID:  record.WorkspaceID,
		AdAccountID:  record.AdAccountID,
		PageID:       record.PageID,
		Name:         record.Name,
		LastPolledAt: record.LastPolledAt,
	}
}
