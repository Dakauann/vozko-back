package unofficial_whatsapp_campaign_repository

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/campaign"
	"vozko/domain/shared"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/database/schema"
)

type repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) uwc.Repository { return &repository{db: db} }

func (r *repository) Create(c *uwc.Campaign) error {
	return r.db.Create(toRow(c)).Error
}

func (r *repository) Update(campaignID string, c *uwc.Campaign) error {
	// An explicit column map rather than Save(): a struct update would write
	// every zero-valued field, and the ones this method must never touch —
	// status, the confirmation codes, created_at — are exactly the ones that are
	// empty on an update payload.
	updates := map[string]interface{}{
		"name":                   c.Name,
		"message_kind":           string(c.Message.Kind),
		"message":                encodeMessage(c.Message),
		"agent_id":               ptr(c.AgentID),
		"workflow_id":            ptr(c.WorkflowID),
		"pipeline_id":            ptr(c.PipelineID),
		"enable_agent_responses": c.EnableAgentResponses,
		"enable_workflow":        c.EnableWorkflow,
		"enable_analysis":        c.EnableAnalysis,
		"enable_auto_staging":    c.EnableAutoStaging,
		"enable_auto_memory":     c.EnableAutoMemory,
		"prefer_audio":           c.PreferAudio,
		"ai_model":               c.AiModel,
		"send_delay_min_ms":      c.SendDelayMinMS,
		"send_delay_max_ms":      c.SendDelayMaxMS,
		"daily_cap":              c.DailyCap,
		"archived":               c.Archived,
		"instance_id":            c.InstanceID,
		"updated_at":             time.Now().UTC(),
	}
	if c.DepartmentID != "" {
		updates["department_id"] = c.DepartmentID
	}
	if !c.ScheduledStart.IsZero() {
		updates["scheduled_start"] = c.ScheduledStart
	}

	return r.db.Model(&schema.UnofficialWhatsAppCampaign{}).
		Where("id = ?", campaignID).Updates(updates).Error
}

func (r *repository) Delete(campaignID string) error {
	return r.db.Where("id = ?", campaignID).
		Delete(&schema.UnofficialWhatsAppCampaign{}).Error
}

func (r *repository) FindByID(campaignID string) (*uwc.Campaign, error) {
	var row schema.UnofficialWhatsAppCampaign
	if err := r.db.Where("id = ?", campaignID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, uwc.ErrCampaignNotFound
		}
		return nil, err
	}
	return toDomain(&row), nil
}

func (r *repository) List(input uwc.ListCampaignsInput) (*shared.PaginatedResult[*uwc.Campaign], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	var total int64
	if err := applyFilters(r.db.Model(&schema.UnofficialWhatsAppCampaign{}), input).
		Count(&total).Error; err != nil {
		return nil, err
	}

	query := applyFilters(r.db.Model(&schema.UnofficialWhatsAppCampaign{}), input).
		Offset(pagination.Offset()).Limit(pagination.PageSize)
	query = applySorts(query, input.Options.Sorts)

	var rows []schema.UnofficialWhatsAppCampaign
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]*uwc.Campaign, 0, len(rows))
	for i := range rows {
		items = append(items, toDomain(&rows[i]))
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *repository) ListByStatus(status campaign.Status) ([]*uwc.Campaign, error) {
	var rows []schema.UnofficialWhatsAppCampaign
	if err := r.db.Where("status = ?", string(status)).Find(&rows).Error; err != nil {
		return nil, err
	}
	return mapRows(rows), nil
}

// ListRunningByInstance backs the circuit breaker: when WhatsApp restricts a
// number, EVERY campaign on it has to stop, not only the one that noticed.
func (r *repository) ListRunningByInstance(instanceID string) ([]*uwc.Campaign, error) {
	var rows []schema.UnofficialWhatsAppCampaign
	if err := r.db.
		Where("instance_id = ? AND status = ?", instanceID, string(campaign.StatusRunning)).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return mapRows(rows), nil
}

func (r *repository) ListScheduledToStart(at time.Time, limit int) ([]*uwc.Campaign, error) {
	var rows []schema.UnofficialWhatsAppCampaign
	err := r.db.
		Where("scheduled_start IS NOT NULL AND scheduled_start <= ?", at.UTC()).
		Where("scheduled_start > ?", time.Time{}).
		Where("status = ?", string(campaign.StatusStopped)).
		Where("archived = ?", false).
		Order("scheduled_start ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return mapRows(rows), nil
}

// UpdateStatus is a compare-and-swap when allowed is non-empty.
//
// The bool reports whether a row actually changed, which is what lets two
// replicas race to complete the same campaign and have exactly one of them win.
func (r *repository) UpdateStatus(campaignID string, status campaign.Status, allowed ...campaign.Status) (bool, error) {
	query := r.db.Model(&schema.UnofficialWhatsAppCampaign{}).Where("id = ?", campaignID)
	if len(allowed) > 0 {
		current := make([]string, 0, len(allowed))
		for _, s := range allowed {
			current = append(current, string(s))
		}
		query = query.Where("status IN ?", current)
	}

	updates := map[string]interface{}{
		"status":     string(status),
		"updated_at": time.Now().UTC(),
	}
	// Leaving a stale reason on a campaign an operator just restarted would
	// show "paused: WhatsApp restricted this number" next to a RUNNING chip.
	if status == campaign.StatusRunning {
		updates["status_reason"] = ""
	}

	result := query.Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *repository) UpdateStatusReason(campaignID, reason string) error {
	return r.db.Model(&schema.UnofficialWhatsAppCampaign{}).
		Where("id = ?", campaignID).
		Update("status_reason", reason).Error
}

func (r *repository) UpdateResetCode(campaignID, code string) error {
	return r.db.Model(&schema.UnofficialWhatsAppCampaign{}).
		Where("id = ?", campaignID).Update("reset_code", code).Error
}

func (r *repository) UpdateClearCode(campaignID, code string) error {
	return r.db.Model(&schema.UnofficialWhatsAppCampaign{}).
		Where("id = ?", campaignID).Update("clear_code", code).Error
}

func mapRows(rows []schema.UnofficialWhatsAppCampaign) []*uwc.Campaign {
	out := make([]*uwc.Campaign, 0, len(rows))
	for i := range rows {
		out = append(out, toDomain(&rows[i]))
	}
	return out
}

func applyFilters(db *gorm.DB, input uwc.ListCampaignsInput) *gorm.DB {
	if input.WorkspaceID != "" {
		db = db.Where("workspace_id = ?", input.WorkspaceID)
	}
	if len(input.DepartmentIDs) > 0 {
		db = db.Where("department_id IN ?", input.DepartmentIDs)
	}
	if len(input.InstanceIDs) > 0 {
		db = db.Where("instance_id IN ?", input.InstanceIDs)
	}
	if input.Status != "" {
		db = db.Where("status = ?", string(input.Status))
	}
	if search := strings.TrimSpace(input.Search); search != "" {
		db = db.Where("name ILIKE ?", "%"+search+"%")
	}
	// Archived defaults to "not archived" rather than "all": every list screen
	// asks for the active set, and an archived campaign appearing in it is the
	// bug the official channel shipped once already.
	if input.Archived != nil {
		db = db.Where("archived = ?", *input.Archived)
	} else {
		db = db.Where("archived = ?", false)
	}
	return db
}

// sortableColumns is an allowlist, not a passthrough: a sort key is
// caller-supplied and interpolating one into ORDER BY is an injection.
var sortableColumns = map[string]string{
	"name":       "name",
	"status":     "status",
	"createdAt":  "created_at",
	"updatedAt":  "updated_at",
	"created_at": "created_at",
	"updated_at": "updated_at",
}

func applySorts(db *gorm.DB, sorts []shared.Sort) *gorm.DB {
	applied := false
	for _, sort := range sorts {
		column, ok := sortableColumns[sort.Field]
		if !ok {
			continue
		}
		direction := "ASC"
		if strings.EqualFold(string(sort.Direction), "desc") {
			direction = "DESC"
		}
		db = db.Order(column + " " + direction)
		applied = true
	}
	if !applied {
		db = db.Order("created_at DESC")
	}
	return db
}
