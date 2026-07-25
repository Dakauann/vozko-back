package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"vozko/domain/agent"
	"vozko/domain/shared"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

type storedToolBinding struct {
	Name       string                 `json:"name"`
	Visibility []string               `json:"visibility,omitempty"`
	ExternalID string                 `json:"externalId,omitempty"`
	Config     map[string]interface{} `json:"config,omitempty"`
}

type storedToolBindings struct {
	Bindings []storedToolBinding `json:"bindings"`
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) agent.Repository {
	return &repository{db: db}
}

func (r *repository) Create(a *agent.Agent) error {
	if a == nil {
		return agent.ErrAgentNameRequired
	}

	serialized := mapAgentToSchema(a)
	if err := r.db.Create(serialized).Error; err != nil {
		return err
	}

	if len(a.KnowledgeBaseIDs) > 0 {
		links := make([]schema.AgentKnowledgeBase, 0, len(a.KnowledgeBaseIDs))
		for _, kbID := range a.KnowledgeBaseIDs {
			links = append(links, schema.AgentKnowledgeBase{AgentID: a.ID, KnowledgeBaseID: kbID})
		}
		if err := r.db.Create(&links).Error; err != nil {
			return err
		}
	}

	if len(a.MCPCollectionIDs) > 0 {
		links := make([]schema.AgentMCPCollection, 0, len(a.MCPCollectionIDs))
		for _, cid := range a.MCPCollectionIDs {
			links = append(links, schema.AgentMCPCollection{AgentID: a.ID, CollectionID: cid})
		}
		if err := r.db.Create(&links).Error; err != nil {
			return err
		}
	}

	return nil
}

func (r *repository) Update(agentID string, a *agent.Agent) error {
	if a == nil {
		return agent.ErrAgentNameRequired
	}

	update := map[string]interface{}{
		"name":                 a.Name,
		"description":          a.Description,
		"department_id":        nilIfEmpty(a.DepartmentID),
		"initial_message":      a.InitialMessage,
		"use_initial_message":  a.UseInitialMessage,
		"messaging_prompt":     a.MessagingPrompt,
		"messaging_model":      a.MessagingModel,
		"avatar_url":           a.AvatarURL,
		"tags":                 encodeStrings(a.Tags),
		"metadata":             encodeMetadata(a.Metadata),
		"tool_bindings":        encodeToolBindings(a.InternalTools, a.ToolBindings),
		"is_active":            a.IsActive,
		"rag_enabled":          a.RAGEnabled,
		"rag_config":           encodeRAGConfig(a.RAGConfig),
		"archived":             a.Archived,
		"provider":             string(a.Provider),
		"external_id":          a.ExternalID,
		"whatsapp_template_id": a.WhatsAppTemplateID,
		"business_phone_id":    nilIfEmpty(a.BusinessPhoneID),
		"media_ids":            encodeStrings(a.MediaIDs),
		"variables":            encodeAgentVariables(a.Variables),
	}

	result := r.db.Model(&schema.Agent{}).Where("id = ?", agentID).Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return agent.ErrAgentNotFound
	}

	if a.KnowledgeBaseIDs != nil {
		if err := r.db.Where("agent_id = ?", agentID).Delete(&schema.AgentKnowledgeBase{}).Error; err != nil {
			return err
		}
		if len(a.KnowledgeBaseIDs) > 0 {
			links := make([]schema.AgentKnowledgeBase, 0, len(a.KnowledgeBaseIDs))
			for _, kbID := range a.KnowledgeBaseIDs {
				links = append(links, schema.AgentKnowledgeBase{AgentID: agentID, KnowledgeBaseID: kbID})
			}
			if err := r.db.Create(&links).Error; err != nil {
				return err
			}
		}
	}

	if a.MCPCollectionIDs != nil {
		if err := r.db.Where("agent_id = ?", agentID).Delete(&schema.AgentMCPCollection{}).Error; err != nil {
			return err
		}
		if len(a.MCPCollectionIDs) > 0 {
			links := make([]schema.AgentMCPCollection, 0, len(a.MCPCollectionIDs))
			for _, cid := range a.MCPCollectionIDs {
				links = append(links, schema.AgentMCPCollection{AgentID: agentID, CollectionID: cid})
			}
			if err := r.db.Create(&links).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *repository) Delete(agentID string) error {
	result := r.db.Delete(&schema.Agent{}, "id = ?", agentID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return agent.ErrAgentNotFound
	}
	return nil
}

func (r *repository) FindByID(agentID string) (*agent.Agent, error) {
	var dbAgent schema.Agent
	if err := r.db.Where("id = ?", agentID).First(&dbAgent).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, agent.ErrAgentNotFound
		}
		return nil, err
	}
	result, err := mapAgentToDomain(&dbAgent)
	if err != nil {
		return nil, err
	}

	var links []schema.AgentKnowledgeBase
	if err := r.db.Where("agent_id = ?", agentID).Find(&links).Error; err == nil {
		ids := make([]string, 0, len(links))
		for _, l := range links {
			ids = append(ids, l.KnowledgeBaseID)
		}
		result.KnowledgeBaseIDs = ids
	}

	var mcpLinks []schema.AgentMCPCollection
	if err := r.db.Where("agent_id = ?", agentID).Find(&mcpLinks).Error; err == nil {
		ids := make([]string, 0, len(mcpLinks))
		for _, l := range mcpLinks {
			ids = append(ids, l.CollectionID)
		}
		result.MCPCollectionIDs = ids
	}

	return result, nil
}

// FindByIDs batch-loads only the display fields (id, name, avatar, active) for the
// given agents in a single query. It deliberately skips the KB/MCP link sub-queries
// that FindByID runs, so inbox enrichment stays O(1 query) per page.
func (r *repository) FindByIDs(agentIDs []string) ([]*agent.Agent, error) {
	if len(agentIDs) == 0 {
		return nil, nil
	}
	var rows []struct {
		ID        string
		Name      string
		AvatarURL string
		IsActive  bool
	}
	if err := r.db.Model(&schema.Agent{}).
		Select("id", "name", "avatar_url", "is_active").
		Where("id IN ?", agentIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	agents := make([]*agent.Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, &agent.Agent{
			ID:        row.ID,
			Name:      row.Name,
			AvatarURL: row.AvatarURL,
			IsActive:  row.IsActive,
		})
	}
	return agents, nil
}

type agentListRow struct {
	ID             string    `gorm:"column:id"`
	WorkspaceID    string    `gorm:"column:workspace_id"`
	DepartmentID   *string   `gorm:"column:department_id"`
	Name           string    `gorm:"column:name"`
	AvatarURL      string    `gorm:"column:avatar_url"`
	Tags           string    `gorm:"column:tags"`
	Provider       string    `gorm:"column:provider"`
	MessagingModel string    `gorm:"column:messaging_model"`
	IsActive       bool      `gorm:"column:is_active"`
	Archived       bool      `gorm:"column:archived"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (r *repository) List(input agent.ListAgentsInput) (*shared.PaginatedResult[*agent.AgentListItem], error) {
	pagination := shared.NormalizePagination(input.Options.Pagination)

	countQuery := r.db.Model(&schema.Agent{})
	countQuery = applyAgentFilters(countQuery, input)

	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, err
	}

	dataQuery := r.db.Model(&schema.Agent{}).
		Select("id", "workspace_id", "department_id", "name", "avatar_url",
			"tags", "provider", "messaging_model",
			"is_active", "archived", "created_at", "updated_at").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize)

	dataQuery = applyAgentFilters(dataQuery, input)
	dataQuery = applyAgentSorts(dataQuery, input.Options.Sorts)

	var rows []agentListRow
	if err := dataQuery.Scan(&rows).Error; err != nil {
		return nil, err
	}

	items := make([]*agent.AgentListItem, 0, len(rows))
	for i := range rows {
		items = append(items, mapAgentRowToListItem(&rows[i]))
	}

	return shared.NewPaginatedResult(items, pagination, total), nil
}

func mapAgentRowToListItem(row *agentListRow) *agent.AgentListItem {
	if row == nil {
		return nil
	}
	return &agent.AgentListItem{
		ID:             row.ID,
		WorkspaceID:    row.WorkspaceID,
		DepartmentID:   derefString(row.DepartmentID),
		Name:           row.Name,
		AvatarURL:      row.AvatarURL,
		Tags:           decodeStringSlice(row.Tags),
		Provider:       agent.AgentProvider(row.Provider),
		MessagingModel: row.MessagingModel,
		IsActive:       row.IsActive,
		Archived:       row.Archived,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func mapAgentToSchema(a *agent.Agent) *schema.Agent {
	metadata := encodeMetadata(a.Metadata)
	tags := encodeStrings(a.Tags)
	toolBindings := encodeToolBindings(a.InternalTools, a.ToolBindings)
	mediaIDs := encodeStrings(a.MediaIDs)
	variables := encodeAgentVariables(a.Variables)

	return &schema.Agent{
		ID:                 a.ID,
		WorkspaceID:        a.WorkspaceID,
		DepartmentID:       nilIfEmpty(a.DepartmentID),
		Name:               a.Name,
		Description:        a.Description,
		InitialMessage:     a.InitialMessage,
		UseInitialMessage:  a.UseInitialMessage,
		MessagingPrompt:    a.MessagingPrompt,
		MessagingModel:     a.MessagingModel,
		AvatarURL:          a.AvatarURL,
		Tags:               tags,
		Metadata:           metadata,
		ToolBindings:       toolBindings,
		Provider:           string(a.Provider),
		ExternalID:         a.ExternalID,
		WhatsAppTemplateID: a.WhatsAppTemplateID,
		BusinessPhoneID:    nilIfEmpty(a.BusinessPhoneID),
		RAGEnabled:         a.RAGEnabled,
		RAGConfig:          encodeRAGConfig(a.RAGConfig),
		IsActive:           a.IsActive,
		Archived:           a.Archived,
		MediaIDs:           mediaIDs,
		Variables:          variables,
	}
}

func mapAgentToDomain(dbAgent *schema.Agent) (*agent.Agent, error) {
	if dbAgent == nil {
		return nil, nil
	}

	metadata := decodeStringMap(dbAgent.Metadata)
	tags := decodeStringSlice(dbAgent.Tags)
	internalTools, toolBindings := decodeToolBindings(dbAgent.ToolBindings)
	mediaIDs := decodeStringSlice(dbAgent.MediaIDs)
	variables := decodeAgentVariables(dbAgent.Variables)

	provider := strings.TrimSpace(dbAgent.Provider)
	if provider == "" {
		provider = string(agent.AgentProviderAI)
	}

	result := &agent.Agent{
		ID:                 dbAgent.ID,
		WorkspaceID:        dbAgent.WorkspaceID,
		DepartmentID:       derefString(dbAgent.DepartmentID),
		Name:               dbAgent.Name,
		Description:        dbAgent.Description,
		InitialMessage:     dbAgent.InitialMessage,
		UseInitialMessage:  dbAgent.UseInitialMessage,
		MessagingPrompt:    dbAgent.MessagingPrompt,
		MessagingModel:     dbAgent.MessagingModel,
		AvatarURL:          dbAgent.AvatarURL,
		Tags:               tags,
		Metadata:           metadata,
		InternalTools:      internalTools,
		ToolBindings:       toolBindings,
		Provider:           agent.AgentProvider(provider),
		ExternalID:         dbAgent.ExternalID,
		WhatsAppTemplateID: dbAgent.WhatsAppTemplateID,
		BusinessPhoneID:    derefString(dbAgent.BusinessPhoneID),
		RAGEnabled:         dbAgent.RAGEnabled,
		RAGConfig:          decodeRAGConfig(dbAgent.RAGConfig),
		IsActive:           dbAgent.IsActive,
		Archived:           dbAgent.Archived,
		MediaIDs:           mediaIDs,
		Variables:          variables,
		CreatedAt:          dbAgent.CreatedAt,
		UpdatedAt:          dbAgent.UpdatedAt,
	}
	return result, nil
}

func encodeMetadata(metadata map[string]string) string {
	if metadata == nil {
		metadata = make(map[string]string)
	}
	bytes, _ := json.Marshal(metadata)
	return string(bytes)
}

func encodeStrings(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	bytes, _ := json.Marshal(values)
	return string(bytes)
}

func encodeStringMap(values map[string]string) string {
	if values == nil {
		values = make(map[string]string)
	}
	bytes, _ := json.Marshal(values)
	return string(bytes)
}

func decodeStringMap(raw string) map[string]string {
	result := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return result
	}
	_ = json.Unmarshal([]byte(raw), &result)
	return result
}

func decodeStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	return values
}

func encodeToolBindings(tools []agent.ToolBinding, externalIDs map[string]string) string {
	stored := storedToolBindings{
		Bindings: make([]storedToolBinding, 0, len(tools)),
	}

	for _, tb := range tools {
		stb := storedToolBinding{
			Name: tb.Name,
		}
		if len(tb.Visibility) > 0 {
			stb.Visibility = make([]string, len(tb.Visibility))
			for i, v := range tb.Visibility {
				stb.Visibility[i] = string(v)
			}
		}
		if len(tb.Config) > 0 {
			stb.Config = tb.Config
		}
		if externalIDs != nil {
			if extID, ok := externalIDs[tb.Name]; ok {
				stb.ExternalID = extID
			}
		}
		stored.Bindings = append(stored.Bindings, stb)
	}

	bytes, _ := json.Marshal(stored)
	return string(bytes)
}

func decodeToolBindings(raw string) ([]agent.ToolBinding, map[string]string) {
	if strings.TrimSpace(raw) == "" {
		return []agent.ToolBinding{}, make(map[string]string)
	}

	var stored storedToolBindings
	if err := json.Unmarshal([]byte(raw), &stored); err == nil && len(stored.Bindings) > 0 {
		tools := make([]agent.ToolBinding, 0, len(stored.Bindings))
		externalIDs := make(map[string]string)

		for _, stb := range stored.Bindings {
			tb := agent.ToolBinding{Name: stb.Name}
			if len(stb.Visibility) > 0 {
				tb.Visibility = make([]agent.ToolVisibility, len(stb.Visibility))
				for i, v := range stb.Visibility {
					tb.Visibility[i] = agent.ToolVisibility(v)
				}
			}
			if len(stb.Config) > 0 {
				tb.Config = stb.Config
			}
			tools = append(tools, tb)
			if stb.ExternalID != "" {
				externalIDs[stb.Name] = stb.ExternalID
			}
		}
		return tools, externalIDs
	}

	legacy := make(map[string]string)
	if err := json.Unmarshal([]byte(raw), &legacy); err == nil && len(legacy) > 0 {
		tools := make([]agent.ToolBinding, 0, len(legacy))
		for name := range legacy {
			tools = append(tools, agent.ToolBinding{Name: name})
		}
		return tools, legacy
	}

	return []agent.ToolBinding{}, make(map[string]string)
}

func applyAgentFilters(db *gorm.DB, input agent.ListAgentsInput) *gorm.DB {
	query := db

	if userID := strings.TrimSpace(input.WorkspaceID); userID != "" {
		query = query.Where("workspace_id = ?", userID)
	}

	if len(input.DepartmentIDs) > 0 {
		query = query.Where("department_id IN ?", input.DepartmentIDs)
	}

	if trimmed := strings.TrimSpace(input.Search); trimmed != "" {
		pattern := "%" + strings.ToLower(trimmed) + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(messaging_prompt) LIKE ?", pattern, pattern)
	}

	if len(input.Tags) > 0 {
		for _, tag := range input.Tags {
			t := strings.TrimSpace(strings.ToLower(tag))
			if t == "" {
				continue
			}
			query = query.Where("EXISTS (SELECT 1 FROM jsonb_array_elements_text(tags::jsonb) AS tag WHERE LOWER(tag) = ?)", t)
		}
	}

	if input.IsActive != nil {
		query = query.Where("is_active = ?", *input.IsActive)
	}

	if input.Archived != nil {
		query = query.Where("archived = ?", *input.Archived)
	}

	if input.CreatedAfter != nil {
		query = query.Where("created_at >= ?", *input.CreatedAfter)
	}

	if input.CreatedBefore != nil {
		query = query.Where("created_at <= ?", *input.CreatedBefore)
	}

	return query
}

func encodeAgentVariables(vars []agent.AgentVariable) string {
	if len(vars) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(vars)
	return string(b)
}

func decodeAgentVariables(raw string) []agent.AgentVariable {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var vars []agent.AgentVariable
	if err := json.Unmarshal([]byte(raw), &vars); err != nil {
		return nil
	}
	return vars
}

func encodeRAGConfig(cfg *agent.RAGConfig) *string {
	if cfg == nil {
		return nil
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}

func decodeRAGConfig(raw *string) *agent.RAGConfig {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil
	}
	var cfg agent.RAGConfig
	if err := json.Unmarshal([]byte(*raw), &cfg); err != nil {
		return nil
	}
	return &cfg
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func applyAgentSorts(db *gorm.DB, sorts []shared.Sort) *gorm.DB {
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
