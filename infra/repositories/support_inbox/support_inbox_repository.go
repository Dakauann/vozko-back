package support_inbox_repository

import (
	"encoding/json"
	"strings"

	"gorm.io/gorm"

	"vozko/domain/shared"
	si "vozko/domain/support_inbox"
	"vozko/infra/database/schema"
)

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) si.Repository {
	return &repository{db: db}
}

func (r *repository) Create(inbox *si.SupportInbox) error {
	var agentID *string
	if inbox.AgentID != "" {
		agentID = &inbox.AgentID
	}
	record := schema.SupportInbox{
		ID:                   inbox.ID,
		WorkspaceID:          inbox.WorkspaceID,
		Name:                 inbox.Name,
		AgentID:              agentID,
		EnableAgentResponses: inbox.EnableAgentResponses,
		GreetingMessage:      inbox.GreetingMessage,
		WidgetColor:          inbox.WidgetColor,
		AllowedOrigins:       schema.StringArray(inbox.AllowedOrigins),
		PreChatFields:        marshalPreChatFields(inbox.PreChatFields),
		Archived:             inbox.Archived,
	}
	return r.db.Create(&record).Error
}

func (r *repository) Update(id string, inbox *si.SupportInbox) error {
	var agentID *string
	if inbox.AgentID != "" {
		agentID = &inbox.AgentID
	}
	update := map[string]interface{}{
		"name":                   inbox.Name,
		"agent_id":               agentID,
		"enable_agent_responses": inbox.EnableAgentResponses,
		"greeting_message":       inbox.GreetingMessage,
		"widget_color":           inbox.WidgetColor,
		"allowed_origins":        schema.StringArray(inbox.AllowedOrigins),
		"pre_chat_fields":        marshalPreChatFields(inbox.PreChatFields),
		"archived":               inbox.Archived,
	}
	result := r.db.Model(&schema.SupportInbox{}).Where("id = ?", id).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return si.ErrInboxNotFound
	}
	return nil
}

func (r *repository) Delete(id string) error {
	result := r.db.Delete(&schema.SupportInbox{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return si.ErrInboxNotFound
	}
	return nil
}

func (r *repository) FindByID(id string) (*si.SupportInbox, error) {
	var record schema.SupportInbox
	if err := r.db.Where("id = ?", id).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, si.ErrInboxNotFound
		}
		return nil, err
	}
	return mapToDomain(&record), nil
}

func (r *repository) List(input si.ListInboxesInput) (*shared.PaginatedResult[*si.SupportInbox], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.SupportInbox{})
	countQuery = applyFilters(countQuery, input)

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := r.db.Model(&schema.SupportInbox{}).
		Offset(pagination.Offset()).
		Limit(pagination.PageSize)
	dataQuery = applyFilters(dataQuery, input)
	dataQuery = applySorts(dataQuery, input.Options.Sorts)

	var records []schema.SupportInbox
	if err := dataQuery.Find(&records).Error; err != nil {
		return nil, err
	}

	items := make([]*si.SupportInbox, 0, len(records))
	for i := range records {
		items = append(items, mapToDomain(&records[i]))
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func mapToDomain(record *schema.SupportInbox) *si.SupportInbox {
	if record == nil {
		return nil
	}
	agentID := ""
	if record.AgentID != nil {
		agentID = *record.AgentID
	}
	origins := []string(record.AllowedOrigins)
	if origins == nil {
		origins = []string{}
	}
	return &si.SupportInbox{
		ID:                   record.ID,
		WorkspaceID:          record.WorkspaceID,
		Name:                 record.Name,
		AgentID:              agentID,
		EnableAgentResponses: record.EnableAgentResponses,
		GreetingMessage:      record.GreetingMessage,
		WidgetColor:          record.WidgetColor,
		AllowedOrigins:       origins,
		PreChatFields:        unmarshalPreChatFields(record.PreChatFields),
		Archived:             record.Archived,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
}

func marshalPreChatFields(fields []si.PreChatField) schema.JSONRaw {
	if len(fields) == 0 {
		return schema.JSONRaw("[]")
	}
	b, err := json.Marshal(fields)
	if err != nil {
		return schema.JSONRaw("[]")
	}
	return schema.JSONRaw(b)
}

func unmarshalPreChatFields(raw schema.JSONRaw) []si.PreChatField {
	if len(raw) == 0 {
		return nil
	}
	var fields []si.PreChatField
	if err := json.Unmarshal(raw, &fields); err == nil && len(fields) > 0 {
		return fields
	}
	return nil
}

func applyFilters(db *gorm.DB, input si.ListInboxesInput) *gorm.DB {
	query := db
	if trimmed := strings.TrimSpace(input.WorkspaceID); trimmed != "" {
		query = query.Where("workspace_id = ?", trimmed)
	}
	if input.Archived != nil {
		query = query.Where("archived = ?", *input.Archived)
	}
	if trimmed := strings.TrimSpace(input.Search); trimmed != "" {
		pattern := "%" + strings.ToLower(trimmed) + "%"
		query = query.Where("LOWER(name) LIKE ?", pattern)
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
