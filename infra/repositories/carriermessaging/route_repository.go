package carriermessaging_repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"vozko/domain/carriermessaging"
	"vozko/domain/sms"
	"vozko/infra/database/schema"
)

type routeRepository struct {
	db *gorm.DB
}

func NewRouteRepository(db *gorm.DB) carriermessaging.RouteRepository {
	return &routeRepository{db: db}
}

func validRoute(kind sms.RouteKind, status sms.RouteStatus) error {
	return errors.Join(
		valid("route kind", kind),
		valid("route status", status),
	)
}

func toRouteSchema(r *sms.Route) (*schema.SMSRoute, error) {
	if err := validRoute(r.Kind, r.Status); err != nil {
		return nil, err
	}
	return &schema.SMSRoute{
		ID:          r.ID,
		WorkspaceID: r.WorkspaceID,
		BrandPrefix: r.BrandPrefix,
		Kind:        string(r.Kind),
		Number:      r.Number,
		Status:      string(r.Status),
		PauseReason: r.PauseReason,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,

		ChannelAutomationColumns: toAutomationColumns(r.Automation),
		ChannelContainerColumns:  schema.ChannelContainerColumns{DepartmentID: r.DepartmentID, PipelineID: r.PipelineID},
	}, nil
}

func toRoute(r *schema.SMSRoute) (*sms.Route, error) {
	route := &sms.Route{
		ID:           r.ID,
		WorkspaceID:  r.WorkspaceID,
		BrandPrefix:  r.BrandPrefix,
		Kind:         sms.RouteKind(r.Kind),
		Number:       r.Number,
		Status:       sms.RouteStatus(r.Status),
		PauseReason:  r.PauseReason,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		Automation:   toAutomation(r.ChannelAutomationColumns),
		DepartmentID: r.DepartmentID,
		PipelineID:   r.PipelineID,
	}
	if err := validRoute(route.Kind, route.Status); err != nil {
		return nil, fmt.Errorf("sms route %s: %w", r.ID, err)
	}
	return route, nil
}

func (r *routeRepository) Create(ctx context.Context, route *sms.Route) error {
	record, err := toRouteSchema(route)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	record.ID = newID(record.ID)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(record).Error; err != nil {
		return err
	}
	route.ID, route.CreatedAt, route.UpdatedAt = record.ID, record.CreatedAt, record.UpdatedAt
	return nil
}

func (r *routeRepository) Update(ctx context.Context, route *sms.Route) error {
	record, err := toRouteSchema(route)
	if err != nil {
		return err
	}
	record.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(record).
		Where("workspace_id = ?", route.WorkspaceID).
		Select("*").
		Omit("id", "workspace_id", "created_at", "deleted_at").
		Updates(record)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return sms.ErrRouteNotFound
	}
	route.UpdatedAt = record.UpdatedAt
	return nil
}

func (r *routeRepository) first(ctx context.Context, query string, args ...any) (*sms.Route, error) {
	var record schema.SMSRoute
	if err := r.db.WithContext(ctx).Where(query, args...).Take(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, sms.ErrRouteNotFound
		}
		return nil, err
	}
	return toRoute(&record)
}

func (r *routeRepository) FindByID(ctx context.Context, workspaceID, id string) (*sms.Route, error) {
	return r.first(ctx, "id = ? AND workspace_id = ?", id, workspaceID)
}

func (r *routeRepository) FindByIDUnscoped(ctx context.Context, id string) (*sms.Route, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *routeRepository) FindByNumber(ctx context.Context, number string) (*sms.Route, error) {
	return r.first(ctx, "kind = ? AND number = ?", sms.RouteDedicated, number)
}

func (r *routeRepository) list(query *gorm.DB) ([]*sms.Route, error) {
	var records []schema.SMSRoute
	if err := query.Order("created_at DESC").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make([]*sms.Route, 0, len(records))
	for i := range records {
		route, err := toRoute(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, route)
	}
	return out, nil
}

func (r *routeRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]*sms.Route, error) {
	return r.list(r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID))
}

func (r *routeRepository) ListAll(ctx context.Context) ([]*sms.Route, error) {
	return r.list(r.db.WithContext(ctx))
}

func (r *routeRepository) Delete(ctx context.Context, workspaceID, id string) error {
	result := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).Delete(&schema.SMSRoute{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return sms.ErrRouteNotFound
	}
	return nil
}
