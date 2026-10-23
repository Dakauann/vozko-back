package campaignaudience_repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/campaign"
	audience "vozko/domain/campaignaudience"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

const insertBatch = 1000

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

var _ campaignaudience_usecase.Lists = (*Repository)(nil)

func validID(id string) bool {
	return len(database.UUIDArray([]string{strings.TrimSpace(id)})) == 1
}

func encode(field string, v any) (datatypes.JSON, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("campaign audience: encoding %s: %w", field, err)
	}
	return datatypes.JSON(raw), nil
}

func encodeOptional(field string, v any, present bool) (datatypes.JSON, error) {
	if !present {
		return nil, nil
	}
	return encode(field, v)
}

func decode(field string, raw datatypes.JSON, into any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("campaign audience: decoding %s: %w", field, err)
	}
	return nil
}

func listRow(l audience.List) (schema.CampaignAudienceList, error) {
	layout, err := encode("layout", l.Layout)
	if err != nil {
		return schema.CampaignAudienceList{}, err
	}
	counts, err := encode("issue counts", l.IssueCounts)
	if err != nil {
		return schema.CampaignAudienceList{}, err
	}
	issues, err := encode("issues", l.Issues)
	if err != nil {
		return schema.CampaignAudienceList{}, err
	}
	return schema.CampaignAudienceList{
		ID: l.ID, WorkspaceID: l.WorkspaceID, CreatedBy: l.CreatedBy, Channel: string(l.Channel), FileName: l.FileName,
		Layout: layout, Lines: l.Lines, IssueCounts: counts, Issues: issues, AttachedTo: schema.OptionalText(l.AttachedTo),
		ExpiresAt: l.ExpiresAt, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}, nil
}

func listOf(r schema.CampaignAudienceList) (*audience.List, error) {
	l := &audience.List{
		ID: r.ID, WorkspaceID: r.WorkspaceID, CreatedBy: r.CreatedBy, Channel: audience.Channel(r.Channel), FileName: r.FileName,
		Lines: r.Lines, AttachedTo: string(r.AttachedTo), ExpiresAt: r.ExpiresAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		IssueCounts: map[campaign.ListIssueReason]int{},
	}
	if err := decode("layout", r.Layout, &l.Layout); err != nil {
		return nil, err
	}
	if err := decode("issue counts", r.IssueCounts, &l.IssueCounts); err != nil {
		return nil, err
	}
	if err := decode("issues", r.Issues, &l.Issues); err != nil {
		return nil, err
	}
	return l, nil
}

func rowRecord(r audience.Row) (schema.CampaignAudienceListRow, error) {
	cells, err := encodeOptional("cells", r.Cells, r.Cells != nil)
	if err != nil {
		return schema.CampaignAudienceListRow{}, err
	}
	variables, err := encodeOptional("variables", r.Variables, r.Variables != nil)
	if err != nil {
		return schema.CampaignAudienceListRow{}, err
	}
	metadata, err := encodeOptional("metadata", r.Metadata, r.Metadata != nil)
	if err != nil {
		return schema.CampaignAudienceListRow{}, err
	}
	return schema.CampaignAudienceListRow{ID: r.ID, ListID: r.ListID, Line: r.Line, Contact: r.Contact, Name: r.Name, Cells: cells, Variables: variables, Metadata: metadata}, nil
}

func rowOf(r schema.CampaignAudienceListRow) (audience.Row, error) {
	row := audience.Row{ID: r.ID, ListID: r.ListID, ListRow: campaign.ListRow{Line: r.Line, Contact: r.Contact, Name: r.Name}}
	if err := decode("cells", r.Cells, &row.Cells); err != nil {
		return audience.Row{}, err
	}
	if err := decode("variables", r.Variables, &row.Variables); err != nil {
		return audience.Row{}, err
	}
	if err := decode("metadata", r.Metadata, &row.Metadata); err != nil {
		return audience.Row{}, err
	}
	return row, nil
}

func rowsOf(records []schema.CampaignAudienceListRow) ([]audience.Row, error) {
	out := make([]audience.Row, 0, len(records))
	for _, r := range records {
		row, err := rowOf(r)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

func (r *Repository) Create(ctx context.Context, l audience.List, rows []audience.Row) error {
	record, err := listRow(l)
	if err != nil {
		return err
	}
	records := make([]schema.CampaignAudienceListRow, 0, len(rows))
	for _, row := range rows {
		rec, err := rowRecord(row)
		if err != nil {
			return err
		}
		records = append(records, rec)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		return tx.CreateInBatches(&records, insertBatch).Error
	})
}

func (r *Repository) Get(ctx context.Context, workspaceID, id string) (*audience.List, error) {
	if !validID(id) || !validID(workspaceID) {
		return nil, audience.ErrListNotFound
	}
	var record schema.CampaignAudienceList
	err := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", strings.TrimSpace(id), strings.TrimSpace(workspaceID)).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, audience.ErrListNotFound
	}
	if err != nil {
		return nil, err
	}
	return listOf(record)
}

func (r *Repository) Save(ctx context.Context, l audience.List) error {
	res := r.db.WithContext(ctx).Model(&schema.CampaignAudienceList{}).Where("id = ? AND workspace_id = ?", l.ID, l.WorkspaceID).
		Updates(map[string]any{"attached_to": schema.OptionalText(l.AttachedTo), "expires_at": l.ExpiresAt, "updated_at": l.UpdatedAt})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return audience.ErrListNotFound
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, workspaceID, id string) error {
	if !validID(id) || !validID(workspaceID) {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.CampaignAudienceList{})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		return tx.Where("list_id = ?", id).Delete(&schema.CampaignAudienceListRow{}).Error
	})
}

func (r *Repository) RowsAfter(ctx context.Context, listID string, after audience.RowCursor, limit int) ([]audience.Row, error) {
	if !validID(listID) {
		return nil, nil
	}
	q := r.db.WithContext(ctx).Where("list_id = ?", listID)
	if validID(after.ID) {
		q = q.Where("(line > ? OR (line = ? AND id > ?::uuid))", after.Line, after.Line, after.ID)
	} else {
		q = q.Where("line > ?", after.Line)
	}
	var records []schema.CampaignAudienceListRow
	if err := q.Order("line, id").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	return rowsOf(records)
}

func (r *Repository) SearchRows(ctx context.Context, listID string, query audience.RowQuery) ([]audience.Row, int, error) {
	if !validID(listID) {
		return nil, 0, nil
	}
	q := r.db.WithContext(ctx).Model(&schema.CampaignAudienceListRow{}).Where("list_id = ?", listID)
	if term := strings.TrimSpace(query.Search); term != "" {
		pattern := sqlexpr.LikeContains(term)
		q = q.Where("(contact ILIKE ? OR name ILIKE ?)", pattern, pattern)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []schema.CampaignAudienceListRow
	if err := q.Order("line, id").Offset(query.Offset).Limit(query.Limit).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	rows, err := rowsOf(records)
	return rows, int(total), err
}

func (r *Repository) CountRows(ctx context.Context, listID string) (int, error) {
	if !validID(listID) {
		return 0, nil
	}
	var total int64
	err := r.db.WithContext(ctx).Model(&schema.CampaignAudienceListRow{}).Where("list_id = ?", listID).Count(&total).Error
	return int(total), err
}

func (r *Repository) Row(ctx context.Context, listID, rowID string) (audience.Row, error) {
	if !validID(listID) || !validID(rowID) {
		return audience.Row{}, audience.ErrRowNotFound
	}
	var record schema.CampaignAudienceListRow
	err := r.db.WithContext(ctx).Where("id = ? AND list_id = ?", rowID, listID).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return audience.Row{}, audience.ErrRowNotFound
	}
	if err != nil {
		return audience.Row{}, err
	}
	return rowOf(record)
}

func (r *Repository) AddRow(ctx context.Context, row audience.Row) error {
	record, err := rowRecord(row)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(&record).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return audience.ErrRowDuplicate
		}
		return err
	}
	return nil
}

func (r *Repository) UpdateRow(ctx context.Context, row audience.Row) error {
	if !validID(row.ID) || !validID(row.ListID) {
		return audience.ErrRowNotFound
	}
	record, err := rowRecord(row)
	if err != nil {
		return err
	}
	res := r.db.WithContext(ctx).Model(&schema.CampaignAudienceListRow{}).Where("id = ? AND list_id = ?", row.ID, row.ListID).
		Updates(map[string]any{"contact": record.Contact, "name": record.Name, "variables": record.Variables, "metadata": record.Metadata})
	if res.Error != nil {
		if database.IsUniqueViolation(res.Error) {
			return audience.ErrRowDuplicate
		}
		return res.Error
	}
	if res.RowsAffected == 0 {
		return audience.ErrRowNotFound
	}
	return nil
}

func (r *Repository) DeleteRow(ctx context.Context, listID, rowID string) error {
	if !validID(listID) || !validID(rowID) {
		return audience.ErrRowNotFound
	}
	res := r.db.WithContext(ctx).Where("id = ? AND list_id = ?", rowID, listID).Delete(&schema.CampaignAudienceListRow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return audience.ErrRowNotFound
	}
	return nil
}

func (r *Repository) DeleteExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&schema.CampaignAudienceList{}).
		Where("attached_to IS NULL AND expires_at IS NOT NULL AND expires_at <= ?", now).
		Order("expires_at").Limit(limit).Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("list_id IN ?", ids).Delete(&schema.CampaignAudienceListRow{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ? AND attached_to IS NULL", ids).Delete(&schema.CampaignAudienceList{}).Error
	})
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}

type Suggestions struct {
	db  *gorm.DB
	now func() time.Time
}

func NewSuggestions(db *gorm.DB) *Suggestions {
	return &Suggestions{db: db, now: time.Now}
}

var _ campaignaudience_usecase.Suggestions = (*Suggestions)(nil)

func (s *Suggestions) Get(ctx context.Context, workspaceID, templateID string) ([]audience.Suggestion, error) {
	out := []audience.Suggestion{}
	if !validID(workspaceID) || !validID(templateID) {
		return out, nil
	}
	var records []schema.WhatsAppTemplateVariableSuggestion
	if err := s.db.WithContext(ctx).Where("workspace_id = ? AND template_id = ?", workspaceID, templateID).Order("slot").Find(&records).Error; err != nil {
		return nil, err
	}
	for _, r := range records {
		out = append(out, audience.Suggestion{Slot: r.Slot, Source: campaign.BindingSource(r.Source)})
	}
	return out, nil
}

func (s *Suggestions) Replace(ctx context.Context, workspaceID, templateID, actorID string, suggestions []audience.Suggestion) error {
	if !validID(workspaceID) || !validID(templateID) || !validID(actorID) {
		return audience.ErrUnavailable
	}
	now := s.now().UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("workspace_id = ? AND template_id = ?", workspaceID, templateID).Delete(&schema.WhatsAppTemplateVariableSuggestion{}).Error; err != nil {
			return err
		}
		if len(suggestions) == 0 {
			return nil
		}
		records := make([]schema.WhatsAppTemplateVariableSuggestion, 0, len(suggestions))
		for _, sg := range suggestions {
			records = append(records, schema.WhatsAppTemplateVariableSuggestion{WorkspaceID: workspaceID, TemplateID: templateID, Slot: sg.Slot, Source: string(sg.Source), UpdatedBy: actorID, UpdatedAt: now})
		}
		return tx.Create(&records).Error
	})
}
