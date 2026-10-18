package unofficial_whatsapp_group_repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/shared"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/infra/database/schema"
	"vozko/infra/database/sqlexpr"
)

var ErrAnalysisLeaseLost = errors.New("the analysis lease was taken by another worker")

type analysisRepository struct{ db *gorm.DB }

func NewAnalysisRepository(db *gorm.DB) uwg.AnalysisRepository { return &analysisRepository{db: db} }

func (r *analysisRepository) Create(ctx context.Context, a *uwg.Analysis) error {
	row, err := analysisToRow(a)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	a.ID, a.CreatedAt, a.UpdatedAt = row.ID, row.CreatedAt, row.UpdatedAt
	return nil
}

func (r *analysisRepository) Save(ctx context.Context, a *uwg.Analysis) error {
	fields, err := json.Marshal(a.TrackedFields)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppGroupAnalysis{}).
		Where("id = ? AND workspace_id = ?", a.ID, a.WorkspaceID).
		Updates(map[string]any{
			"name":               a.Name,
			"prompt":             a.Prompt,
			"status":             string(a.Status),
			"trigger":            string(a.Trigger),
			"messages_per_cycle": a.MessagesPerCycle,
			"schedule_hour":      a.ScheduleHour,
			"schedule_minute":    a.ScheduleMinute,
			"tracked_fields":     fields,
			"next_run_at":        a.NextRunAt,
			"failures":           a.Failures,
			"paused_reason":      string(a.PausedReason),
			"updated_at":         time.Now().UTC(),
		}).Error
}

func (r *analysisRepository) Delete(ctx context.Context, workspaceID, id string) error {
	res := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).
		Delete(&schema.UnofficialWhatsAppGroupAnalysis{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return uwg.ErrAnalysisNotFound
	}
	return nil
}

func (r *analysisRepository) FindByID(ctx context.Context, workspaceID, id string) (*uwg.Analysis, error) {
	var row schema.UnofficialWhatsAppGroupAnalysis
	err := r.db.WithContext(ctx).Where("id = ? AND workspace_id = ?", id, workspaceID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, uwg.ErrAnalysisNotFound
	}
	if err != nil {
		return nil, err
	}
	return analysisToDomain(&row)
}

func (r *analysisRepository) List(ctx context.Context, f uwg.AnalysisFilter) (*shared.PaginatedResult[*uwg.AnalysisRow], error) {
	pagination := shared.NormalizePagination(f.Pagination)
	where, args := scopeSQL("wg", f.Scope)
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Unscoped().Table("unofficial_whatsapp_group_analyses a").
			Joins("JOIN unofficial_whatsapp_workspace_groups wg ON wg.id = a.group_id AND wg.deleted_at IS NULL").
			Where("a.workspace_id = ? AND a.deleted_at IS NULL", f.WorkspaceID).
			Where(where, args...)
		if f.Status.Valid() {
			q = q.Where("a.status = ?", string(f.Status))
		}
		if f.Trigger.Valid() {
			q = q.Where("a.trigger = ?", string(f.Trigger))
		}
		if f.GroupID != "" {
			q = q.Where("a.group_id = ?", f.GroupID)
		}
		if search := strings.TrimSpace(f.Search); search != "" {
			like := sqlexpr.LikeContains(search)
			q = q.Where(`(a.name ILIKE ? ESCAPE '\' OR a.prompt ILIKE ? ESCAPE '\' OR wg.subject ILIKE ? ESCAPE '\')`, like, like, like)
		}
		return q
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, err
	}
	type row struct {
		schema.UnofficialWhatsAppGroupAnalysis
		GroupSubject string
		Runs         int64
	}
	var rows []row
	order := "a.created_at DESC, a.id"
	switch f.Sort {
	case uwg.AnalysisByOldest:
		order = "a.created_at ASC, a.id"
	case uwg.AnalysisByName:
		order = "lower(a.name) ASC, a.id"
	case uwg.AnalysisByNameZA:
		order = "lower(a.name) DESC, a.id"
	}
	if err := base().
		Select(`a.*, wg.subject AS group_subject,
			(SELECT COUNT(*) FROM unofficial_whatsapp_group_analysis_runs r WHERE r.analysis_id = a.id) AS runs`).
		Order(order).Offset(pagination.Offset()).Limit(pagination.PageSize).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*uwg.AnalysisRow, 0, len(rows))
	for i := range rows {
		a, err := analysisToDomain(&rows[i].UnofficialWhatsAppGroupAnalysis)
		if err != nil {
			return nil, err
		}
		items = append(items, &uwg.AnalysisRow{Analysis: *a, GroupSubject: rows[i].GroupSubject, Runs: rows[i].Runs})
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *analysisRepository) AddPending(ctx context.Context, groupID string, n int) error {
	if n <= 0 {
		return nil
	}
	return r.db.WithContext(ctx).Exec(`
		UPDATE unofficial_whatsapp_group_analyses
		   SET pending_messages = pending_messages + ?
		 WHERE group_id = ? AND deleted_at IS NULL AND status = 'active' AND trigger = 'message_count'`,
		n, groupID).Error
}

func (r *analysisRepository) ClaimNext(ctx context.Context, now time.Time, owner string, leaseUntil time.Time) (*uwg.Analysis, error) {
	var rows []schema.UnofficialWhatsAppGroupAnalysis
	err := r.db.WithContext(ctx).Raw(`
		UPDATE unofficial_whatsapp_group_analyses
		   SET lease_owner = ?, lease_until = ?
		 WHERE id IN (
		       SELECT id FROM unofficial_whatsapp_group_analyses
		        WHERE deleted_at IS NULL AND status = 'active'
		          AND ((trigger = 'message_count' AND pending_messages >= messages_per_cycle AND (next_run_at IS NULL OR next_run_at <= ?))
		            OR (trigger = 'scheduled' AND next_run_at <= ?))
		          AND (lease_until IS NULL OR lease_until < ?)
		        ORDER BY COALESCE(next_run_at, updated_at) ASC
		        LIMIT 1
		        FOR UPDATE SKIP LOCKED)
		RETURNING *`, owner, leaseUntil, now, now, now).Scan(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return analysisToDomain(&rows[0])
}

func (r *analysisRepository) Extend(ctx context.Context, id, owner string, leaseUntil time.Time) error {
	res := r.db.WithContext(ctx).Exec(`UPDATE unofficial_whatsapp_group_analyses SET lease_until = ? WHERE id = ? AND lease_owner = ?`,
		leaseUntil, id, owner)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAnalysisLeaseLost
	}
	return nil
}

func (r *analysisRepository) Finish(ctx context.Context, a *uwg.Analysis, owner string, consumed int) error {
	res := r.db.WithContext(ctx).Exec(`
		UPDATE unofficial_whatsapp_group_analyses
		   SET pending_messages = GREATEST(pending_messages - ?, 0),
		       analyzed_until = ?, analyzed_until_id = ?, last_run_at = ?, next_run_at = ?,
		       failures = ?, paused_reason = ?, status = ?,
		       lease_owner = '', lease_until = NULL
		 WHERE id = ? AND lease_owner = ?`,
		consumed, a.AnalyzedUntil, a.AnalyzedUntilID, a.LastRunAt, a.NextRunAt,
		a.Failures, string(a.PausedReason), string(a.Status), a.ID, owner)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrAnalysisLeaseLost
	}
	return nil
}

func (r *analysisRepository) Counts(ctx context.Context, s uwg.StatsScope) (int64, int64, error) {
	where, args := scopeSQL("wg", s.Scope)
	var c struct {
		Total  int64
		Active int64
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*) AS total, COUNT(*) FILTER (WHERE a.status = 'active') AS active
		  FROM unofficial_whatsapp_group_analyses a
		  JOIN unofficial_whatsapp_workspace_groups wg ON wg.id = a.group_id AND wg.deleted_at IS NULL
		 WHERE a.workspace_id = ? AND a.deleted_at IS NULL AND `+where,
		append([]any{s.WorkspaceID}, args...)...).Scan(&c).Error
	return c.Total, c.Active, err
}

func (r *analysisRepository) GroupsWithActive(ctx context.Context, workspaceID string, groupIDs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(groupIDs) == 0 {
		return out, nil
	}
	var ids []string
	err := r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppGroupAnalysis{}).
		Where("workspace_id = ? AND group_id IN ? AND status = 'active'", workspaceID, groupIDs).
		Distinct().Pluck("group_id", &ids).Error
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

func analysisToRow(a *uwg.Analysis) (schema.UnofficialWhatsAppGroupAnalysis, error) {
	fields, err := json.Marshal(a.TrackedFields)
	if err != nil {
		return schema.UnofficialWhatsAppGroupAnalysis{}, err
	}
	return schema.UnofficialWhatsAppGroupAnalysis{
		ID: a.ID, WorkspaceID: a.WorkspaceID, GroupID: a.GroupID, Name: a.Name, Prompt: a.Prompt,
		Status: string(a.Status), Trigger: string(a.Trigger), MessagesPerCycle: a.MessagesPerCycle,
		ScheduleHour: a.ScheduleHour, ScheduleMinute: a.ScheduleMinute, TrackedFields: fields,
		PendingMessages: a.PendingMessages, AnalyzedUntil: a.AnalyzedUntil, AnalyzedUntilID: a.AnalyzedUntilID,
		Failures: a.Failures, PausedReason: string(a.PausedReason), LastRunAt: a.LastRunAt,
		NextRunAt: a.NextRunAt, CreatedByID: optional(a.CreatedByID),
	}, nil
}

func analysisToDomain(r *schema.UnofficialWhatsAppGroupAnalysis) (*uwg.Analysis, error) {
	fields := []uwg.TrackedField{}
	if len(r.TrackedFields) > 0 {
		if err := json.Unmarshal(r.TrackedFields, &fields); err != nil {
			return nil, err
		}
	}
	return &uwg.Analysis{
		ID: r.ID, WorkspaceID: r.WorkspaceID, GroupID: r.GroupID, Name: r.Name, Prompt: r.Prompt,
		Status: uwg.AnalysisStatus(r.Status), Trigger: uwg.Trigger(r.Trigger), MessagesPerCycle: r.MessagesPerCycle,
		ScheduleHour: r.ScheduleHour, ScheduleMinute: r.ScheduleMinute, TrackedFields: fields,
		PendingMessages: r.PendingMessages, AnalyzedUntil: r.AnalyzedUntil, AnalyzedUntilID: r.AnalyzedUntilID,
		Failures: r.Failures, PausedReason: uwg.PausedReason(r.PausedReason), LastRunAt: r.LastRunAt,
		NextRunAt: r.NextRunAt, CreatedByID: value(r.CreatedByID), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}
