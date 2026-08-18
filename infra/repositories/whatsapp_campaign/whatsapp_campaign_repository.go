package whatsapp_campaign_repository

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/shared"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) wc.Repository {
	return &repository{db: db}
}

func (r *repository) Create(c *wc.Campaign) error {
	var agentID *string
	if c.AgentID != "" {
		agentID = &c.AgentID
	}

	var workflowID *string
	if c.WorkflowID != "" {
		workflowID = &c.WorkflowID
	}

	var businessPhoneID *string
	if c.BusinessPhoneID != "" {
		businessPhoneID = &c.BusinessPhoneID
	}

	var departmentID *string
	if c.DepartmentID != "" {
		departmentID = &c.DepartmentID
	}

	var templateID *string
	if c.TemplateID != "" {
		templateID = &c.TemplateID
	}

	var pipelineID *string
	if c.PipelineID != "" {
		pipelineID = &c.PipelineID
	}

	campaignType := string(c.Type)
	if campaignType == "" {
		campaignType = string(wc.CampaignTypeStandard)
	}

	record := schema.WhatsAppCampaign{
		ID:                   c.ID,
		WorkspaceID:          c.WorkspaceID,
		DepartmentID:         departmentID,
		BusinessPhoneID:      businessPhoneID,
		Name:                 c.Name,
		Type:                 campaignType,
		TemplateID:           templateID,
		AgentID:              agentID,
		WorkflowID:           workflowID,
		PipelineID:           pipelineID,
		EnableAgentResponses: c.EnableAgentResponses,
		EnableWorkflow:       c.EnableWorkflow,
		EnableAnalysis:       c.EnableAnalysis,
		EnableAutoStaging:    c.EnableAutoStaging,
		EnableAutoMemory:     c.EnableAutoMemory,
		PreferAudio:          c.PreferAudio,
		ShowTemplateInCrm:    c.ShowTemplateInCrm,
		AiModel:              c.AiModel,
		Status:               string(c.Status),
		ScheduledStart:       c.ScheduledStart,
	}
	return r.db.Create(&record).Error
}

func (r *repository) Update(campaignID string, c *wc.Campaign) error {
	var agentID *string
	if c.AgentID != "" {
		agentID = &c.AgentID
	}

	var workflowID *string
	if c.WorkflowID != "" {
		workflowID = &c.WorkflowID
	}

	var businessPhoneID *string
	if c.BusinessPhoneID != "" {
		businessPhoneID = &c.BusinessPhoneID
	}

	var departmentID *string
	if c.DepartmentID != "" {
		departmentID = &c.DepartmentID
	}

	var templateID *string
	if c.TemplateID != "" {
		templateID = &c.TemplateID
	}

	var pipelineID *string
	if c.PipelineID != "" {
		pipelineID = &c.PipelineID
	}

	update := map[string]interface{}{
		"name":                   c.Name,
		"department_id":          departmentID,
		"template_id":            templateID,
		"agent_id":               agentID,
		"workflow_id":            workflowID,
		"pipeline_id":            pipelineID,
		"business_phone_id":      businessPhoneID,
		"enable_agent_responses": c.EnableAgentResponses,
		"enable_workflow":        c.EnableWorkflow,
		"enable_analysis":        c.EnableAnalysis,
		"enable_auto_staging":    c.EnableAutoStaging,
		"enable_auto_memory":     c.EnableAutoMemory,
		"prefer_audio":           c.PreferAudio,
		"show_template_in_crm":   c.ShowTemplateInCrm,
		"ai_model":               c.AiModel,
		"archived":               c.Archived,
		"scheduled_start":        c.ScheduledStart,
	}
	result := r.db.Model(&schema.WhatsAppCampaign{}).Where("id = ?", campaignID).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wc.ErrCampaignNotFound
	}
	return nil
}

func (r *repository) Delete(campaignID string) error {
	result := r.db.Delete(&schema.WhatsAppCampaign{}, "id = ?", campaignID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wc.ErrCampaignNotFound
	}
	return nil
}

func (r *repository) FindByID(campaignID string) (*wc.Campaign, error) {
	var record schema.WhatsAppCampaign
	if err := r.db.Where("id = ?", campaignID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, wc.ErrCampaignNotFound
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) List(input wc.ListCampaignsInput) (*shared.PaginatedResult[*wc.Campaign], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.WhatsAppCampaign{})
	countQuery = applyFilters(countQuery, input)

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := r.db.Model(&schema.WhatsAppCampaign{}).
		Offset(pagination.Offset()).
		Limit(pagination.PageSize)

	dataQuery = applyFilters(dataQuery, input)
	dataQuery = applySorts(dataQuery, input.Options.Sorts)

	var records []schema.WhatsAppCampaign
	if err := dataQuery.Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*wc.Campaign, 0, len(records))
	for i := range records {
		items = append(items, mapToDomain(&records[i]))
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *repository) ListByStatus(status wc.Status) ([]*wc.Campaign, error) {
	var records []schema.WhatsAppCampaign
	if err := r.db.Where("status = ?", string(status)).Find(&records).Error; err != nil {
		return nil, err
	}
	campaigns := make([]*wc.Campaign, 0, len(records))
	for i := range records {
		campaigns = append(campaigns, mapToDomain(&records[i]))
	}
	return campaigns, nil
}

func (r *repository) ListScheduledToStart(at time.Time, limit int) ([]*wc.Campaign, error) {
	query := r.db.Model(&schema.WhatsAppCampaign{}).
		Where("scheduled_start <= ?", at.UTC()).
		Where("scheduled_start > ?", time.Time{}).
		Where("status <> ?", string(wc.CampaignStatusRunning)).
		Order("scheduled_start ASC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	var records []schema.WhatsAppCampaign
	if err := query.Find(&records).Error; err != nil {
		return nil, err
	}

	campaigns := make([]*wc.Campaign, 0, len(records))
	for i := range records {
		campaigns = append(campaigns, mapToDomain(&records[i]))
	}

	return campaigns, nil
}

func (r *repository) UpdateStatus(campaignID string, status wc.Status, allowed ...wc.Status) (bool, error) {
	if campaignID == "" {
		return false, wc.ErrCampaignNotFound
	}
	if !status.IsValid() {
		return false, wc.ErrCampaignStatusInvalid
	}

	query := r.db.Model(&schema.WhatsAppCampaign{}).Where("id = ?", campaignID)
	if len(allowed) > 0 {
		values := make([]string, 0, len(allowed))
		for _, item := range allowed {
			if !item.IsValid() {
				return false, wc.ErrCampaignStatusInvalid
			}
			values = append(values, string(item))
		}
		query = query.Where("status IN ?", values)
	}

	result := query.Updates(&schema.WhatsAppCampaign{Status: string(status)})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *repository) UpdateResetCode(campaignID string, resetCode string) error {
	if campaignID == "" {
		return wc.ErrCampaignNotFound
	}
	result := r.db.Model(&schema.WhatsAppCampaign{}).
		Where("id = ?", campaignID).
		Update("reset_code", resetCode)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wc.ErrCampaignNotFound
	}
	return nil
}

func (r *repository) UpdateClearCode(campaignID string, clearCode string) error {
	if campaignID == "" {
		return wc.ErrCampaignNotFound
	}
	result := r.db.Model(&schema.WhatsAppCampaign{}).
		Where("id = ?", campaignID).
		Update("clear_code", clearCode)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wc.ErrCampaignNotFound
	}
	return nil
}

func (r *repository) FindLatestOrganicByBusinessPhone(workspaceID string, businessPhoneID string) (*wc.Campaign, error) {
	var record schema.WhatsAppCampaign
	query := r.db.Where("type = ?", string(wc.CampaignTypeOrganic)).
		Where("archived = ?", false).
		Order("created_at DESC")

	if workspaceID != "" {
		query = query.Where("workspace_id = ?", workspaceID)
	}

	if businessPhoneID != "" {
		query = query.Where("business_phone_id = ?", businessPhoneID)
	}

	if err := query.First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, wc.ErrCampaignNotFound
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func mapToDomain(record *schema.WhatsAppCampaign) *wc.Campaign {
	if record == nil {
		return nil
	}
	agentID := ""
	if record.AgentID != nil {
		agentID = *record.AgentID
	}

	workflowID := ""
	if record.WorkflowID != nil {
		workflowID = *record.WorkflowID
	}

	businessPhoneID := ""
	if record.BusinessPhoneID != nil {
		businessPhoneID = *record.BusinessPhoneID
	}

	templateID := ""
	if record.TemplateID != nil {
		templateID = *record.TemplateID
	}

	departmentID := ""
	if record.DepartmentID != nil {
		departmentID = *record.DepartmentID
	}

	pipelineID := ""
	if record.PipelineID != nil {
		pipelineID = *record.PipelineID
	}

	return &wc.Campaign{
		ID:                   record.ID,
		WorkspaceID:          record.WorkspaceID,
		DepartmentID:         departmentID,
		BusinessPhoneID:      businessPhoneID,
		Name:                 record.Name,
		Type:                 wc.CampaignType(record.Type),
		TemplateID:           templateID,
		AgentID:              agentID,
		WorkflowID:           workflowID,
		PipelineID:           pipelineID,
		EnableAgentResponses: record.EnableAgentResponses,
		EnableWorkflow:       record.EnableWorkflow,
		EnableAnalysis:       record.EnableAnalysis,
		EnableAutoStaging:    record.EnableAutoStaging,
		EnableAutoMemory:     record.EnableAutoMemory,
		PreferAudio:          record.PreferAudio,
		ShowTemplateInCrm:    record.ShowTemplateInCrm,
		AiModel:              record.AiModel,
		Status:               normalizeCampaignStatus(record.Status),
		ResetCode:            record.ResetCode,
		ClearCode:            record.ClearCode,
		Archived:             record.Archived,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
		ScheduledStart:       record.ScheduledStart,
	}
}

func normalizeCampaignStatus(value string) wc.Status {
	status := wc.Status(strings.ToUpper(strings.TrimSpace(value)))
	if status.IsValid() {
		return status
	}
	return wc.CampaignStatusStopped
}

func applyFilters(db *gorm.DB, input wc.ListCampaignsInput) *gorm.DB {
	query := db

	if trimmed := strings.TrimSpace(input.WorkspaceID); trimmed != "" {
		query = query.Where("workspace_id = ?", trimmed)
	}

	if len(input.DepartmentIDs) > 0 {
		query = query.Where("department_id IN ?", input.DepartmentIDs)
	}

	if input.Archived != nil {
		query = query.Where("archived = ?", *input.Archived)
	}

	if trimmed := strings.TrimSpace(input.Search); trimmed != "" {
		pattern := "%" + strings.ToLower(trimmed) + "%"
		query = query.Where("LOWER(name) LIKE ?", pattern)
	}

	if len(input.ScheduledStart.Values) > 0 {
		firstValue := strings.TrimSpace(input.ScheduledStart.Values[0])
		if firstValue != "" {
			parseTimestamp := func(value string) (time.Time, bool) {
				value = strings.TrimSpace(value)
				if value == "" {
					return time.Time{}, false
				}

				timestamp, err := time.Parse(time.RFC3339, value)
				if err == nil {
					return timestamp, true
				}

				timestamp, err = time.Parse("2006-01-02", value)
				if err == nil {
					return timestamp, true
				}

				return time.Time{}, false
			}

			timestamp, ok := parseTimestamp(firstValue)
			if ok {
				switch input.ScheduledStart.Operator {
				case shared.FilterOpGte:
					query = query.Where("scheduled_start >= ?", timestamp)
				case shared.FilterOpLte:
					query = query.Where("scheduled_start <= ?", timestamp)
				case shared.FilterOpBetween:
					if len(input.ScheduledStart.Values) > 1 {
						endValue := strings.TrimSpace(input.ScheduledStart.Values[1])
						if endValue != "" {
							endTimestamp, endOk := parseTimestamp(endValue)
							if endOk {
								query = query.Where("scheduled_start BETWEEN ? AND ?", timestamp, endTimestamp)
							}
						}
					}
				default:
					query = query.Where("scheduled_start = ?", timestamp)
				}
			}
		}
	}

	if len(input.TemplateIDs) > 0 {
		templates := make([]string, 0, len(input.TemplateIDs))
		for _, id := range input.TemplateIDs {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				templates = append(templates, trimmed)
			}
		}
		if len(templates) > 0 {
			query = query.Where("template_id IN ?", templates)
		}
	}

	if input.Type != "" && input.Type.IsValid() {
		query = query.Where("type = ?", string(input.Type))
	}

	return query
}

func applySorts(db *gorm.DB, sorts []shared.Sort) *gorm.DB {
	if len(sorts) == 0 {
		return db.Order("created_at DESC")
	}

	query := db
	for _, sort := range sorts {
		direction := "ASC"
		if strings.EqualFold(string(sort.Direction), string(shared.SortDesc)) {
			direction = "DESC"
		}

		switch strings.ToLower(sort.Field) {
		case "name":
			query = query.Order("name " + direction)
		case "createdat":
			query = query.Order("created_at " + direction)
		case "updatedat":
			query = query.Order("updated_at " + direction)
		default:
			continue
		}
	}

	return query
}
