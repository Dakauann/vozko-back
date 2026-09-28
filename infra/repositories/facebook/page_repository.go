package facebook_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	fbdomain "vozko/domain/facebook"
	"vozko/domain/shared"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type pageRepository struct {
	db *gorm.DB
}

func NewPageRepository(db *gorm.DB) fbdomain.PageRepository {
	return &pageRepository{db: db}
}

func (r *pageRepository) Create(ctx context.Context, p *fbdomain.Page) error {
	record := toPageSchema(p)
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		if database.IsUniqueViolation(err) {
			return fbdomain.ErrPageAlreadyLinked
		}
		return err
	}
	p.ID = record.ID
	return nil
}

func (r *pageRepository) Update(ctx context.Context, p *fbdomain.Page) error {
	updates := map[string]any{
		"workspace_id":        p.WorkspaceID,
		"grant_id":            p.GrantID,
		"name":                p.Name,
		"username":            p.Username,
		"category":            p.Category,
		"link":                p.Link,
		"followers_count":     p.FollowersCount,
		"linked_ig_user_id":   p.LinkedIGUserID,
		"tasks":               joinTasks(p.Tasks),
		"granted_scopes":      strings.Join(p.GrantedScopes, ","),
		"status":              string(p.Status),
		"status_reason":       p.StatusReason,
		"picture_storage_key": p.PictureStorageKey,
	}
	if p.PageToken != "" {
		updates["page_token"] = piigorm.NewEncrypted(p.PageToken)
	}
	return r.update(ctx, p.ID, updates)
}

func (r *pageRepository) UpdateConfig(ctx context.Context, p *fbdomain.Page) error {
	return r.update(ctx, p.ID, map[string]any{
		"department_id":          p.DepartmentID,
		"agent_id":               p.AgentID,
		"workflow_id":            p.WorkflowID,
		"pipeline_id":            p.PipelineID,
		"enable_agent_responses": p.EnableAgentResponses,
		"enable_workflow":        p.EnableWorkflow,
		"enable_analysis":        p.EnableAnalysis,
		"enable_auto_staging":    p.EnableAutoStaging,
		"enable_auto_memory":     p.EnableAutoMemory,
		"automation_disclosure":  p.AutomationDisclosure,
	})
}

func (r *pageRepository) UpdateStatus(ctx context.Context, id string, status fbdomain.Status, reason string) error {
	return r.update(ctx, id, map[string]any{"status": string(status), "status_reason": reason})
}

func (r *pageRepository) UpdateToken(ctx context.Context, id, grantID, pageToken string, scopes []string, tasks []fbdomain.Task) error {
	return r.update(ctx, id, map[string]any{
		"grant_id":       grantID,
		"page_token":     piigorm.NewEncrypted(pageToken),
		"granted_scopes": strings.Join(scopes, ","),
		"tasks":          joinTasks(tasks),
	})
}

func (r *pageRepository) UpdateSubscription(ctx context.Context, id string, fields []string, at time.Time) error {
	return r.update(ctx, id, map[string]any{"subscribed_fields": strings.Join(fields, ","), "webhook_subscribed_at": at})
}

func (r *pageRepository) UpdateRouting(ctx context.Context, id string, isDefault *bool, at time.Time) error {
	return r.update(ctx, id, map[string]any{"is_default_route_app": isDefault, "routing_checked_at": at})
}

func (r *pageRepository) UpdatePolicy(ctx context.Context, id, action, reason string, at time.Time) error {
	return r.update(ctx, id, map[string]any{"policy_action": action, "policy_reason": reason, "policy_at": at})
}

func (r *pageRepository) UpdateProfile(ctx context.Context, id string, remote *fbdomain.RemotePage, pictureKey string, at time.Time) error {
	updates := map[string]any{
		"name":              remote.Name,
		"username":          remote.Username,
		"category":          remote.Category,
		"link":              remote.Link,
		"followers_count":   remote.FollowersCount,
		"linked_ig_user_id": remote.LinkedIGUserID,
		"health_checked_at": at,
	}
	if pictureKey != "" {
		updates["picture_storage_key"] = pictureKey
	}
	return r.update(ctx, id, updates)
}

func (r *pageRepository) FindByID(ctx context.Context, id string) (*fbdomain.Page, error) {
	return r.first(r.db.WithContext(ctx), "id = ?", id)
}

func (r *pageRepository) FindByFBPageID(ctx context.Context, fbPageID string) (*fbdomain.Page, error) {
	return r.first(r.db.WithContext(ctx), "fb_page_id = ?", fbPageID)
}

func (r *pageRepository) FindByFBPageIDUnscoped(ctx context.Context, fbPageID string) (*fbdomain.Page, error) {
	return r.first(r.db.WithContext(ctx).Unscoped(), "fb_page_id = ?", fbPageID)
}

func (r *pageRepository) first(q *gorm.DB, where string, arg any) (*fbdomain.Page, error) {
	var record schema.FacebookPage
	if err := q.First(&record, where, arg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fbdomain.ErrPageNotFound
		}
		return nil, err
	}
	return toPageDomain(&record), nil
}

func (r *pageRepository) ListByWorkspace(ctx context.Context, in fbdomain.ListPagesInput) (*shared.PaginatedResult[*fbdomain.Page], error) {
	pagination := shared.NormalizePagination(in.Options.Pagination)
	query := r.db.WithContext(ctx).Model(&schema.FacebookPage{}).Where("workspace_id = ?", in.WorkspaceID)
	if search := strings.TrimSpace(in.Search); search != "" {
		like := "%" + strings.ToLower(search) + "%"
		query = query.Where("LOWER(name) LIKE ? OR LOWER(username) LIKE ?", like, like)
	}
	if in.Status != nil {
		query = query.Where("status = ?", string(*in.Status))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var records []schema.FacebookPage
	if err := query.Order("created_at DESC").Limit(pagination.PageSize).Offset(pagination.Offset()).Find(&records).Error; err != nil {
		return nil, err
	}
	return shared.NewPaginatedResult(toPages(records), pagination, total), nil
}

func (r *pageRepository) ListByGrant(ctx context.Context, grantID string) ([]*fbdomain.Page, error) {
	var records []schema.FacebookPage
	if err := r.db.WithContext(ctx).Find(&records, "grant_id = ?", grantID).Error; err != nil {
		return nil, err
	}
	return toPages(records), nil
}

func (r *pageRepository) ListConnected(ctx context.Context, limit, offset int) ([]*fbdomain.Page, error) {
	var records []schema.FacebookPage
	if err := r.db.WithContext(ctx).
		Where("status = ?", string(fbdomain.StatusConnected)).
		Order("id").Limit(limit).Offset(offset).
		Find(&records).Error; err != nil {
		return nil, err
	}
	return toPages(records), nil
}

func (r *pageRepository) Restore(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Unscoped().Model(&schema.FacebookPage{}).Where("id = ?", id).Update("deleted_at", nil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fbdomain.ErrPageNotFound
	}
	return nil
}

func (r *pageRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Delete(&schema.FacebookPage{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fbdomain.ErrPageNotFound
	}
	return nil
}

func (r *pageRepository) update(ctx context.Context, id string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&schema.FacebookPage{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fbdomain.ErrPageNotFound
	}
	return nil
}

func joinTasks(tasks []fbdomain.Task) string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, string(t))
	}
	return strings.Join(out, ",")
}

func toPages(records []schema.FacebookPage) []*fbdomain.Page {
	out := make([]*fbdomain.Page, 0, len(records))
	for i := range records {
		out = append(out, toPageDomain(&records[i]))
	}
	return out
}

func toPageSchema(p *fbdomain.Page) *schema.FacebookPage {
	return &schema.FacebookPage{
		ID:                   p.ID,
		WorkspaceID:          p.WorkspaceID,
		DepartmentID:         p.DepartmentID,
		GrantID:              p.GrantID,
		FBPageID:             p.FBPageID,
		Name:                 p.Name,
		Username:             p.Username,
		Category:             p.Category,
		Link:                 p.Link,
		PictureStorageKey:    p.PictureStorageKey,
		FollowersCount:       p.FollowersCount,
		LinkedIGUserID:       p.LinkedIGUserID,
		PageToken:            piigorm.NewEncrypted(p.PageToken),
		Tasks:                joinTasks(p.Tasks),
		GrantedScopes:        strings.Join(p.GrantedScopes, ","),
		AgentID:              p.AgentID,
		WorkflowID:           p.WorkflowID,
		PipelineID:           p.PipelineID,
		EnableAgentResponses: p.EnableAgentResponses,
		EnableWorkflow:       p.EnableWorkflow,
		EnableAnalysis:       p.EnableAnalysis,
		EnableAutoStaging:    p.EnableAutoStaging,
		EnableAutoMemory:     p.EnableAutoMemory,
		AutomationDisclosure: p.AutomationDisclosure,
		Status:               string(p.Status),
		StatusReason:         p.StatusReason,
		SubscribedFields:     strings.Join(p.SubscribedFields, ","),
		WebhookSubscribedAt:  p.WebhookSubscribedAt,
		IsDefaultRouteApp:    p.IsDefaultRouteApp,
		RoutingCheckedAt:     p.RoutingCheckedAt,
	}
}

func toPageDomain(record *schema.FacebookPage) *fbdomain.Page {
	tasks := make([]fbdomain.Task, 0)
	for _, t := range splitList(record.Tasks) {
		tasks = append(tasks, fbdomain.Task(t))
	}
	return &fbdomain.Page{
		ID:                   record.ID,
		WorkspaceID:          record.WorkspaceID,
		DepartmentID:         record.DepartmentID,
		GrantID:              record.GrantID,
		FBPageID:             record.FBPageID,
		Name:                 record.Name,
		Username:             record.Username,
		Category:             record.Category,
		Link:                 record.Link,
		PictureStorageKey:    record.PictureStorageKey,
		FollowersCount:       record.FollowersCount,
		LinkedIGUserID:       record.LinkedIGUserID,
		PageToken:            record.PageToken.Plain,
		Tasks:                tasks,
		GrantedScopes:        splitList(record.GrantedScopes),
		AgentID:              record.AgentID,
		WorkflowID:           record.WorkflowID,
		PipelineID:           record.PipelineID,
		EnableAgentResponses: record.EnableAgentResponses,
		EnableWorkflow:       record.EnableWorkflow,
		EnableAnalysis:       record.EnableAnalysis,
		EnableAutoStaging:    record.EnableAutoStaging,
		EnableAutoMemory:     record.EnableAutoMemory,
		AutomationDisclosure: record.AutomationDisclosure,
		Status:               fbdomain.Status(record.Status),
		StatusReason:         record.StatusReason,
		SubscribedFields:     splitList(record.SubscribedFields),
		WebhookSubscribedAt:  record.WebhookSubscribedAt,
		IsDefaultRouteApp:    record.IsDefaultRouteApp,
		RoutingCheckedAt:     record.RoutingCheckedAt,
		PolicyAction:         record.PolicyAction,
		PolicyReason:         record.PolicyReason,
		PolicyAt:             record.PolicyAt,
		HealthCheckedAt:      record.HealthCheckedAt,
		CreatedAt:            record.CreatedAt,
		UpdatedAt:            record.UpdatedAt,
	}
}
