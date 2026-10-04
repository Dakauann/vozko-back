package webchat_repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"vozko/domain/shared"
	wcdomain "vozko/domain/webchat"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database/schema"
)

type widgetRepository struct {
	db *gorm.DB
}

func NewWidgetRepository(db *gorm.DB) wcdomain.WidgetRepository {
	return &widgetRepository{db: db}
}

func (r *widgetRepository) Create(ctx context.Context, w *wcdomain.Widget) error {
	record, err := toWidgetSchema(w)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	w.ID = record.ID
	w.CreatedAt = record.CreatedAt
	w.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *widgetRepository) Update(ctx context.Context, w *wcdomain.Widget) error {
	record, err := toWidgetSchema(w)
	if err != nil {
		return err
	}
	update := map[string]any{
		"department_id":          record.DepartmentID,
		"name":                   record.Name,
		"status":                 record.Status,
		"allowed_origins":        record.AllowedOrigins,
		"accent_color":           record.AccentColor,
		"position":               record.Position,
		"launcher_label":         record.LauncherLabel,
		"welcome_title":          record.WelcomeTitle,
		"welcome_message":        record.WelcomeMessage,
		"team_name":              record.TeamName,
		"assistant_name":         record.AssistantName,
		"intake_name":            record.IntakeName,
		"intake_email":           record.IntakeEmail,
		"intake_phone":           record.IntakePhone,
		"privacy_policy_url":     record.PrivacyPolicyURL,
		"default_country_code":   record.DefaultCountryCode,
		"allow_human_request":    record.AllowHumanRequest,
		"allow_attachments":      record.AllowAttachments,
		"identity_mode":          record.IdentityMode,
		"agent_id":               record.AgentID,
		"workflow_id":            record.WorkflowID,
		"pipeline_id":            record.PipelineID,
		"enable_agent_responses": record.EnableAgentResponses,
		"enable_workflow":        record.EnableWorkflow,
		"enable_analysis":        record.EnableAnalysis,
		"enable_auto_staging":    record.EnableAutoStaging,
		"enable_auto_memory":     record.EnableAutoMemory,
	}
	if w.IdentitySecret != "" {
		update["identity_secret"] = record.IdentitySecret
	}
	result := r.db.WithContext(ctx).Model(&schema.WebchatWidget{}).
		Where("id = ? AND workspace_id = ?", w.ID, w.WorkspaceID).
		Updates(update)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wcdomain.ErrWidgetNotFound
	}
	return nil
}

func (r *widgetRepository) FindByID(ctx context.Context, workspaceID, id string) (*wcdomain.Widget, error) {
	return r.first(ctx, "id = ? AND workspace_id = ?", id, workspaceID)
}

func (r *widgetRepository) FindByIDUnscoped(ctx context.Context, id string) (*wcdomain.Widget, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *widgetRepository) FindByPublicKey(ctx context.Context, publicKey string) (*wcdomain.Widget, error) {
	if strings.TrimSpace(publicKey) == "" {
		return nil, wcdomain.ErrWidgetNotFound
	}
	return r.first(ctx, "public_key = ?", publicKey)
}

func (r *widgetRepository) first(ctx context.Context, query string, args ...any) (*wcdomain.Widget, error) {
	var record schema.WebchatWidget
	if err := r.db.WithContext(ctx).Where(query, args...).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wcdomain.ErrWidgetNotFound
		}
		return nil, err
	}
	return toWidgetDomain(&record)
}

func (r *widgetRepository) ListByWorkspace(ctx context.Context, in wcdomain.ListWidgetsInput) (*shared.PaginatedResult[*wcdomain.Widget], error) {
	query := r.db.WithContext(ctx).Model(&schema.WebchatWidget{}).Where("workspace_id = ?", in.WorkspaceID)
	if s := strings.TrimSpace(in.Search); s != "" {
		query = query.Where("LOWER(name) LIKE ?", "%"+strings.ToLower(s)+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	pagination := shared.NormalizePagination(in.Options.Pagination)

	var records []schema.WebchatWidget
	if err := query.Order("created_at DESC").
		Offset((pagination.Page - 1) * pagination.PageSize).Limit(pagination.PageSize).
		Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]*wcdomain.Widget, 0, len(records))
	for i := range records {
		w, err := toWidgetDomain(&records[i])
		if err != nil {
			return nil, err
		}
		items = append(items, w)
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *widgetRepository) Delete(ctx context.Context, workspaceID, id string) error {
	result := r.db.WithContext(ctx).Delete(&schema.WebchatWidget{}, "id = ? AND workspace_id = ?", id, workspaceID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return wcdomain.ErrWidgetNotFound
	}
	return nil
}

func toWidgetSchema(w *wcdomain.Widget) (*schema.WebchatWidget, error) {
	origins, err := json.Marshal(w.AllowedOrigins)
	if err != nil {
		return nil, err
	}
	record := &schema.WebchatWidget{
		ID:                   w.ID,
		WorkspaceID:          w.WorkspaceID,
		DepartmentID:         w.DepartmentID,
		Name:                 w.Name,
		PublicKey:            w.PublicKey,
		Status:               string(w.Status),
		AllowedOrigins:       datatypes.JSON(origins),
		AccentColor:          w.AccentColor,
		Position:             string(w.Position),
		LauncherLabel:        w.LauncherLabel,
		WelcomeTitle:         w.WelcomeTitle,
		WelcomeMessage:       w.WelcomeMessage,
		TeamName:             w.TeamName,
		AssistantName:        w.AssistantName,
		IntakeName:           string(w.IntakeName),
		IntakeEmail:          string(w.IntakeEmail),
		IntakePhone:          string(w.IntakePhone),
		PrivacyPolicyURL:     w.PrivacyPolicyURL,
		DefaultCountryCode:   w.DefaultCountryCode,
		AllowHumanRequest:    w.AllowHumanRequest,
		AllowAttachments:     w.AllowAttachments,
		IdentityMode:         string(w.IdentityMode),
		AgentID:              w.AgentID,
		WorkflowID:           w.WorkflowID,
		PipelineID:           w.PipelineID,
		EnableAgentResponses: w.EnableAgentResponses,
		EnableWorkflow:       w.EnableWorkflow,
		EnableAnalysis:       w.EnableAnalysis,
		EnableAutoStaging:    w.EnableAutoStaging,
		EnableAutoMemory:     w.EnableAutoMemory,
	}
	if w.IdentitySecret != "" {
		record.IdentitySecret = piigorm.NewEncrypted(w.IdentitySecret)
	}
	return record, nil
}

func toWidgetDomain(record *schema.WebchatWidget) (*wcdomain.Widget, error) {
	var origins []string
	if len(record.AllowedOrigins) > 0 {
		if err := json.Unmarshal(record.AllowedOrigins, &origins); err != nil {
			return nil, err
		}
	}
	return &wcdomain.Widget{
		ID:                   record.ID,
		WorkspaceID:          record.WorkspaceID,
		DepartmentID:         record.DepartmentID,
		Name:                 record.Name,
		PublicKey:            record.PublicKey,
		Status:               wcdomain.Status(record.Status),
		AllowedOrigins:       origins,
		AccentColor:          record.AccentColor,
		Position:             wcdomain.Position(record.Position),
		LauncherLabel:        record.LauncherLabel,
		WelcomeTitle:         record.WelcomeTitle,
		WelcomeMessage:       record.WelcomeMessage,
		TeamName:             record.TeamName,
		AssistantName:        record.AssistantName,
		IntakeName:           wcdomain.FieldRule(record.IntakeName),
		IntakeEmail:          wcdomain.FieldRule(record.IntakeEmail),
		IntakePhone:          wcdomain.FieldRule(record.IntakePhone),
		PrivacyPolicyURL:     record.PrivacyPolicyURL,
		DefaultCountryCode:   record.DefaultCountryCode,
		AllowHumanRequest:    record.AllowHumanRequest,
		AllowAttachments:     record.AllowAttachments,
		IdentityMode:         wcdomain.IdentityMode(record.IdentityMode),
		IdentitySecret:       record.IdentitySecret.Plain,
		AgentID:              record.AgentID,
		WorkflowID:           record.WorkflowID,
		PipelineID:           record.PipelineID,
		EnableAgentResponses: record.EnableAgentResponses,
		EnableWorkflow:       record.EnableWorkflow,
		EnableAnalysis:       record.EnableAnalysis,
		EnableAutoStaging:    record.EnableAutoStaging,
		EnableAutoMemory:     record.EnableAutoMemory,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}, nil
}
