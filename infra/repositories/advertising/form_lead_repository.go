package advertising_repository

import (
	"context"

	"gorm.io/gorm"

	"vozko/domain/advertising"
	"vozko/infra/database/schema"
)

type formLeadRepository struct {
	db *gorm.DB
}

func NewFormLeadRepository(db *gorm.DB) advertising.FormLeadRepository {
	return &formLeadRepository{db: db}
}

const saveFormLeadSQL = `INSERT INTO ad_form_leads (meta_id, workspace_id, form_meta_id, ad_meta_id, page_id, answers, lead_id, created_time)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (meta_id) DO NOTHING`

func (r *formLeadRepository) Save(ctx context.Context, lead *advertising.FormLead) (bool, error) {
	if blank(lead.WorkspaceID) {
		return false, advertising.ErrWorkspaceRequired
	}
	if blank(lead.MetaID) {
		return false, errMetaIDRequired
	}
	answers := lead.Answers
	if answers == nil {
		answers = map[string]string{}
	}
	encoded, err := encodeJSON(answers)
	if err != nil {
		return false, err
	}
	var leadID *string
	if !blank(lead.LeadID) {
		leadID = &lead.LeadID
	}
	result := r.db.WithContext(ctx).Exec(saveFormLeadSQL,
		lead.MetaID, lead.WorkspaceID, lead.FormMetaID, lead.AdMetaID, lead.PageID, encoded, leadID, lead.CreatedTime)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *formLeadRepository) LinkLead(ctx context.Context, metaID, leadID string) error {
	result := r.db.WithContext(ctx).Model(&schema.AdFormLead{}).Where("meta_id = ?", metaID).Update("lead_id", leadID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errFormLeadNotFound
	}
	return nil
}

func (r *formLeadRepository) List(ctx context.Context, q advertising.FormLeadQuery) ([]*advertising.FormLead, int64, error) {
	if blank(q.WorkspaceID) {
		return nil, 0, advertising.ErrWorkspaceRequired
	}
	scoped := func() *gorm.DB {
		tx := r.db.WithContext(ctx).Model(&schema.AdFormLead{}).Where("workspace_id = ?", q.WorkspaceID)
		if !blank(q.FormMetaID) {
			tx = tx.Where("form_meta_id = ?", q.FormMetaID)
		}
		return tx
	}
	var total int64
	if err := scoped().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []schema.AdFormLead
	if err := scoped().Order("created_time DESC, meta_id").Limit(q.Limit).Offset(q.Offset).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*advertising.FormLead, 0, len(records))
	for i := range records {
		lead, err := toFormLead(&records[i])
		if err != nil {
			return nil, 0, err
		}
		out = append(out, lead)
	}
	return out, total, nil
}

func toFormLead(record *schema.AdFormLead) (*advertising.FormLead, error) {
	lead := &advertising.FormLead{
		MetaID:      record.MetaID,
		FormMetaID:  record.FormMetaID,
		AdMetaID:    record.AdMetaID,
		PageID:      record.PageID,
		WorkspaceID: record.WorkspaceID,
		CreatedTime: record.CreatedTime,
	}
	if record.LeadID != nil {
		lead.LeadID = *record.LeadID
	}
	if err := decodeJSON(record.Answers, &lead.Answers); err != nil {
		return nil, err
	}
	return lead, nil
}
