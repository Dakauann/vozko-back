package unofficial_whatsapp_group_repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/shared"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

type groupRepository struct{ db *gorm.DB }

func NewGroupRepository(db *gorm.DB) uwg.GroupRepository { return &groupRepository{db: db} }

func (r *groupRepository) UpsertSeen(ctx context.Context, g *uwg.Group) error {
	g.Normalize()
	if err := g.Validate(); err != nil {
		return err
	}
	row := groupToRow(g)
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:     []clause.Column{{Name: "workspace_id"}, {Name: "jid"}},
		TargetWhere: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "deleted_at IS NULL"}}},
		DoUpdates: clause.Assignments(map[string]any{
			"subject":      gorm.Expr("COALESCE(NULLIF(EXCLUDED.subject, ''), unofficial_whatsapp_workspace_groups.subject)"),
			"description":  gorm.Expr("EXCLUDED.description"),
			"announce":     gorm.Expr("EXCLUDED.announce"),
			"locked":       gorm.Expr("EXCLUDED.locked"),
			"member_count": gorm.Expr("CASE WHEN EXCLUDED.member_count > 0 AND unofficial_whatsapp_workspace_groups.followed_at IS NULL THEN EXCLUDED.member_count ELSE unofficial_whatsapp_workspace_groups.member_count END"),
			"updated_at":   time.Now().UTC(),
		}),
	}).Create(&row).Error
	if err != nil {
		return err
	}
	stored, err := r.FindByJID(ctx, g.WorkspaceID, g.JID)
	if err != nil {
		return err
	}
	*g = *stored
	return nil
}

func (r *groupRepository) Save(ctx context.Context, g *uwg.Group) error {
	g.Normalize()
	if err := g.Validate(); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppWorkspaceGroup{}).
		Where("id = ? AND workspace_id = ?", g.ID, g.WorkspaceID).
		Updates(map[string]any{
			"subject":         g.Subject,
			"description":     g.Description,
			"origin":          string(g.Origin),
			"category":        g.Category,
			"announce":        g.Announce,
			"locked":          g.Locked,
			"series_id":       optional(g.SeriesID),
			"series_sequence": g.SeriesSequence,
			"created_by_id":   optional(g.CreatedByID),
			"updated_at":      time.Now().UTC(),
		}).Error
}

func (r *groupRepository) SetFollowed(ctx context.Context, workspaceID, id string, at *time.Time) error {
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppWorkspaceGroup{}).
		Where("id = ? AND workspace_id = ?", id, workspaceID).
		Updates(map[string]any{"followed_at": at, "members_synced_at": nil, "updated_at": time.Now().UTC()}).Error
}

func (r *groupRepository) SetPrimary(ctx context.Context, workspaceID, id, instanceID string) error {
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppWorkspaceGroup{}).
		Where("id = ? AND workspace_id = ?", id, workspaceID).
		Updates(map[string]any{"primary_instance_id": optional(instanceID), "updated_at": time.Now().UTC()}).Error
}

func (r *groupRepository) FindByID(ctx context.Context, workspaceID, id string, scope uwg.Scope) (*uwg.Group, error) {
	groups, err := r.FindByIDs(ctx, workspaceID, []string{id}, scope)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, uwg.ErrGroupNotFound
	}
	return groups[0], nil
}

func (r *groupRepository) FindByJID(ctx context.Context, workspaceID, jid string) (*uwg.Group, error) {
	var row schema.UnofficialWhatsAppWorkspaceGroup
	err := r.db.WithContext(ctx).Where("workspace_id = ? AND jid = ?", workspaceID, jid).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, uwg.ErrGroupNotFound
	}
	if err != nil {
		return nil, err
	}
	return groupToDomain(&row), nil
}

func (r *groupRepository) FindByIDs(ctx context.Context, workspaceID string, ids []string, scope uwg.Scope) ([]*uwg.Group, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	where, args := scopeSQL("wg", scope)
	var rows []schema.UnofficialWhatsAppWorkspaceGroup
	err := r.db.WithContext(ctx).Unscoped().Table("unofficial_whatsapp_workspace_groups wg").
		Where("wg.workspace_id = ? AND wg.id IN ? AND wg.deleted_at IS NULL", workspaceID, ids).
		Where(where, args...).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*uwg.Group, 0, len(rows))
	for i := range rows {
		out = append(out, groupToDomain(&rows[i]))
	}
	return out, nil
}

func (r *groupRepository) Exists(ctx context.Context, workspaceID string) (bool, error) {
	var found int64
	err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM (
		SELECT 1 FROM unofficial_whatsapp_workspace_groups
		 WHERE workspace_id = ? AND deleted_at IS NULL LIMIT 1) any_group`, workspaceID).Scan(&found).Error
	return found > 0, err
}

func (r *groupRepository) List(ctx context.Context, f uwg.GroupFilter) (*shared.PaginatedResult[*uwg.Group], error) {
	pagination := shared.NormalizePagination(f.Pagination)
	base := func() *gorm.DB {
		where, args := scopeSQL("wg", f.Scope)
		q := r.db.WithContext(ctx).Unscoped().Table("unofficial_whatsapp_workspace_groups wg").
			Where("wg.workspace_id = ? AND wg.deleted_at IS NULL", f.WorkspaceID).
			Where(where, args...)
		if search := strings.TrimSpace(f.Search); search != "" {
			like := sqlexpr.LikeContains(search)
			q = q.Where("(wg.subject ILIKE ? ESCAPE '\\' OR wg.description ILIKE ? ESCAPE '\\')", like, like)
		}
		if f.Followed != nil {
			if *f.Followed {
				q = q.Where("wg.followed_at IS NOT NULL")
			} else {
				q = q.Where("wg.followed_at IS NULL")
			}
		}
		if f.Origin.Valid() {
			q = q.Where("wg.origin = ?", string(f.Origin))
		}
		if f.Category != nil {
			q = q.Where("wg.category = ?", *f.Category)
		}
		if f.AdminsOnly != nil {
			q = q.Where("wg.announce = ?", *f.AdminsOnly)
		}
		if f.InstanceID != "" {
			q = q.Where(`EXISTS (SELECT 1 FROM unofficial_whatsapp_groups seat
				WHERE seat.workspace_id = wg.workspace_id AND seat.jid = wg.jid
				  AND seat.deleted_at IS NULL AND seat.instance_id = ?)`, f.InstanceID)
		}
		if f.SeriesID != "" {
			q = q.Where("wg.series_id = ?", f.SeriesID)
		}
		if f.HasAnalysis != nil {
			exists := `EXISTS (SELECT 1 FROM unofficial_whatsapp_group_analyses a
				WHERE a.group_id = wg.id AND a.deleted_at IS NULL AND a.status = 'active')`
			if *f.HasAnalysis {
				q = q.Where(exists)
			} else {
				q = q.Where("NOT " + exists)
			}
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []schema.UnofficialWhatsAppWorkspaceGroup
	if err := base().Select("wg.*").Order(groupOrder(f.Sort)).
		Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*uwg.Group, 0, len(rows))
	for i := range rows {
		items = append(items, groupToDomain(&rows[i]))
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func groupOrder(s uwg.GroupSort) string {
	switch s {
	case uwg.SortOldest:
		return "wg.created_at ASC, wg.id"
	case uwg.SortName:
		return "lower(wg.subject) ASC, wg.id"
	case uwg.SortNameDesc:
		return "lower(wg.subject) DESC, wg.id"
	case uwg.SortMembers:
		return "wg.member_count DESC, wg.id"
	}
	return "wg.followed_at DESC NULLS LAST, wg.created_at DESC, wg.id"
}

func (r *groupRepository) Categories(ctx context.Context, workspaceID string, scope uwg.Scope) ([]string, error) {
	where, args := scopeSQL("wg", scope)
	var out []string
	err := r.db.WithContext(ctx).Unscoped().Table("unofficial_whatsapp_workspace_groups wg").
		Where("wg.workspace_id = ? AND wg.deleted_at IS NULL AND wg.category <> ''", workspaceID).
		Where(where, args...).
		Distinct().Order("category").Pluck("wg.category", &out).Error
	return out, err
}

func (r *groupRepository) NextSeriesSequence(ctx context.Context, workspaceID, seriesID string) (int, error) {
	var next int
	err := r.db.WithContext(ctx).Raw(`SELECT COALESCE(MAX(series_sequence), 0) + 1
		FROM unofficial_whatsapp_workspace_groups
		WHERE workspace_id = ? AND series_id = ? AND deleted_at IS NULL`, workspaceID, seriesID).Scan(&next).Error
	return next, err
}
