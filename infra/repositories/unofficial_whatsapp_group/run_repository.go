package unofficial_whatsapp_group_repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/shared"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

const maxRunsRead = 500

type runRepository struct{ db *gorm.DB }

func NewRunRepository(db *gorm.DB) uwg.RunRepository { return &runRepository{db: db} }

func (r *runRepository) Create(ctx context.Context, run *uwg.Run) error {
	row, err := runToRow(run)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	run.ID = row.ID
	return nil
}

func (r *runRepository) ListByAnalysis(ctx context.Context, workspaceID, analysisID string, p shared.Pagination) (*shared.PaginatedResult[*uwg.Run], error) {
	pagination := shared.NormalizePagination(p)
	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppGroupAnalysisRun{}).
			Where("workspace_id = ? AND analysis_id = ?", workspaceID, analysisID)
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []schema.UnofficialWhatsAppGroupAnalysisRun
	if err := base().Order("completed_at DESC, cycle DESC").
		Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*uwg.Run, 0, len(rows))
	for i := range rows {
		run, err := runToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		items = append(items, run)
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *runRepository) FindByID(ctx context.Context, workspaceID, analysisID, runID string) (*uwg.Run, error) {
	var row schema.UnofficialWhatsAppGroupAnalysisRun
	err := r.db.WithContext(ctx).
		Where("id = ? AND workspace_id = ? AND analysis_id = ?", runID, workspaceID, analysisID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, uwg.ErrAnalysisNotFound
	}
	if err != nil {
		return nil, err
	}
	return runToDomain(&row)
}

func (r *runRepository) Since(ctx context.Context, workspaceID, analysisID string, since time.Time) ([]uwg.Run, error) {
	var rows []schema.UnofficialWhatsAppGroupAnalysisRun
	if err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND analysis_id = ? AND completed_at >= ?", workspaceID, analysisID, since).
		Order("completed_at DESC").Limit(maxRunsRead).Find(&rows).Error; err != nil {
		return nil, err
	}
	return runsToDomain(rows)
}

func (r *runRepository) RecentForGroups(ctx context.Context, workspaceID string, groupIDs []string, since time.Time, perGroup int) (map[string][]uwg.Run, error) {
	out := map[string][]uwg.Run{}
	if len(groupIDs) == 0 {
		return out, nil
	}
	var rows []schema.UnofficialWhatsAppGroupAnalysisRun
	err := r.db.WithContext(ctx).Raw(`
		SELECT * FROM (
		  SELECT r.*, ROW_NUMBER() OVER (PARTITION BY r.group_id ORDER BY r.completed_at DESC) AS rank
		    FROM unofficial_whatsapp_group_analysis_runs r
		   WHERE r.workspace_id = ? AND r.group_id IN ? AND r.completed_at >= ?
		) ranked WHERE rank <= ?`, workspaceID, groupIDs, since, perGroup).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	runs, err := runsToDomain(rows)
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		out[run.GroupID] = append(out[run.GroupID], run)
	}
	return out, nil
}

func (r *runRepository) NextCycle(ctx context.Context, analysisID string) (int, error) {
	var next int
	err := r.db.WithContext(ctx).Raw(`SELECT COALESCE(MAX(cycle), 0) + 1
		FROM unofficial_whatsapp_group_analysis_runs WHERE analysis_id = ?`, analysisID).Scan(&next).Error
	return next, err
}

const runsJoin = `FROM unofficial_whatsapp_group_analysis_runs r
		JOIN unofficial_whatsapp_workspace_groups wg ON wg.id = r.group_id AND wg.deleted_at IS NULL`

func runScope(s uwg.StatsScope) (string, []any) {
	where, args := scopeSQL("wg", s.Scope)
	return `r.workspace_id = ? AND r.status = 'completed' AND ` + where, append([]any{s.WorkspaceID}, args...)
}

func (r *runRepository) Totals(ctx context.Context, s uwg.StatsScope) (uwg.RunTotals, error) {
	where, args := runScope(s)
	var t uwg.RunTotals
	err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) AS runs, COALESCE(SUM(r.messages_analyzed), 0) AS messages,
		COUNT(*) FILTER (WHERE r.alert_level = 'high') AS high_alerts `+runsJoin+` WHERE `+where, args...).Scan(&t).Error
	return t, err
}

func (r *runRepository) SentimentTally(ctx context.Context, s uwg.StatsScope, since time.Time) (uwg.SentimentTally, error) {
	where, args := runScope(s)
	var t uwg.SentimentTally
	err := r.db.WithContext(ctx).Raw(`SELECT COALESCE(SUM(r.positive), 0) AS positive,
		COALESCE(SUM(r.neutral), 0) AS neutral, COALESCE(SUM(r.negative), 0) AS negative `+
		runsJoin+` WHERE `+where+` AND r.completed_at >= ?`, append(args, since)...).Scan(&t).Error
	return t, err
}

func (r *runRepository) SentimentByGroup(ctx context.Context, s uwg.StatsScope, since time.Time, limit int) ([]uwg.GroupSentiment, error) {
	where, args := runScope(s)
	var out []uwg.GroupSentiment
	err := r.db.WithContext(ctx).Raw(`SELECT wg.id AS group_id, wg.subject,
		SUM(r.positive) AS positive, SUM(r.neutral) AS neutral, SUM(r.negative) AS negative `+
		runsJoin+` WHERE `+where+` AND r.completed_at >= ?
		GROUP BY wg.id, wg.subject
		HAVING SUM(r.positive + r.neutral + r.negative) > 0
		ORDER BY SUM(r.positive + r.neutral + r.negative) DESC, wg.subject
		LIMIT ?`, append(args, since, limit)...).Scan(&out).Error
	return out, err
}

func (r *runRepository) Themes(ctx context.Context, f uwg.ThemeFilter) (*shared.PaginatedResult[*uwg.ThemeAggregate], error) {
	pagination := shared.NormalizePagination(f.Pagination)
	where, args := runScope(f.StatsScope)
	args = append(args, f.Since)
	search := ""
	if s := strings.TrimSpace(f.Search); s != "" {
		search = ` AND t.key LIKE ? ESCAPE '\'`
		args = append(args, database.LikeContains(strings.ToLower(s)))
	}
	themes := `WITH themes AS (
		SELECT t.key, t.label, r.group_id, wg.subject, r.completed_at
		  ` + runsJoin + `
		  CROSS JOIN LATERAL unnest(r.theme_keys, r.themes) AS t(key, label)
		 WHERE ` + where + ` AND r.completed_at >= ? AND t.key <> ''` + search + `)`

	var total int64
	if err := r.db.WithContext(ctx).Raw(themes+` SELECT COUNT(DISTINCT key) FROM themes`, args...).Scan(&total).Error; err != nil {
		return nil, err
	}

	type row struct {
		Key         string
		Label       string
		Occurrences int64
		LastSeenAt  time.Time
		Groups      []byte
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(themes+`, ranked AS (
		SELECT key, MODE() WITHIN GROUP (ORDER BY label) AS label, COUNT(*) AS occurrences, MAX(completed_at) AS last_seen_at
		  FROM themes GROUP BY key)
		SELECT ranked.key, ranked.label, ranked.occurrences, ranked.last_seen_at,
		       (SELECT json_agg(json_build_object('groupId', g.group_id, 'subject', g.subject) ORDER BY g.hits DESC)
		          FROM (SELECT group_id, subject, COUNT(*) AS hits FROM themes WHERE themes.key = ranked.key
		                 GROUP BY group_id, subject) g) AS groups
		  FROM ranked
		 ORDER BY ranked.occurrences DESC, ranked.last_seen_at DESC
		 OFFSET ? LIMIT ?`, append(args, pagination.Offset(), pagination.PageSize)...).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]*uwg.ThemeAggregate, 0, len(rows))
	for _, rw := range rows {
		agg := &uwg.ThemeAggregate{Theme: rw.Label, Occurrences: rw.Occurrences, LastSeenAt: rw.LastSeenAt, Groups: []uwg.GroupRef{}}
		if len(rw.Groups) > 0 {
			if err := json.Unmarshal(rw.Groups, &agg.Groups); err != nil {
				return nil, err
			}
		}
		items = append(items, agg)
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *runRepository) RecentInsights(ctx context.Context, s uwg.StatsScope, limit int) ([]uwg.Insight, error) {
	where, args := runScope(s)
	var out []uwg.Insight
	err := r.db.WithContext(ctx).Raw(`SELECT a.id AS analysis_id, a.name AS analysis_name, r.group_id,
		insight.value AS message, r.alert_level, r.completed_at AS created_at `+runsJoin+`
		JOIN unofficial_whatsapp_group_analyses a ON a.id = r.analysis_id
		CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(r.result->'insights', '[]'::jsonb)) AS insight(value)
		WHERE `+where+`
		ORDER BY r.completed_at DESC
		LIMIT ?`, append(args, limit)...).Scan(&out).Error
	return out, err
}

func runToRow(run *uwg.Run) (schema.UnofficialWhatsAppGroupAnalysisRun, error) {
	row := schema.UnofficialWhatsAppGroupAnalysisRun{
		ID: run.ID, AnalysisID: run.AnalysisID, WorkspaceID: run.WorkspaceID, GroupID: run.GroupID,
		Cycle: run.Cycle, Kind: string(run.Kind), Status: string(run.Status), MessagesAnalyzed: run.MessagesAnalyzed,
		PeriodFrom: run.PeriodFrom, PeriodTo: run.PeriodTo, Model: run.Model,
		PromptTokens: run.PromptTokens, CompletionTokens: run.CompletionTokens,
		FailureReason: truncate(run.FailureReason, 500), StartedAt: run.StartedAt, CompletedAt: run.CompletedAt,
		ThemeKeys: pq.StringArray{}, Themes: pq.StringArray{},
	}
	if run.Result != nil {
		body, err := json.Marshal(run.Result)
		if err != nil {
			return row, err
		}
		row.Result = body
		row.Sentiment, row.AlertLevel = string(run.Result.Sentiment), string(run.Result.AlertLevel)
		for _, theme := range run.Result.KeyThemes {
			row.ThemeKeys = append(row.ThemeKeys, strings.ToLower(theme))
			row.Themes = append(row.Themes, theme)
		}
	}
	tally := run.Tally()
	row.Positive, row.Neutral, row.Negative = int(tally.Positive), int(tally.Neutral), int(tally.Negative)
	return row, nil
}

func runToDomain(row *schema.UnofficialWhatsAppGroupAnalysisRun) (*uwg.Run, error) {
	run := &uwg.Run{
		ID: row.ID, AnalysisID: row.AnalysisID, WorkspaceID: row.WorkspaceID, GroupID: row.GroupID,
		Cycle: row.Cycle, Kind: uwg.RunKind(row.Kind), Status: uwg.RunStatus(row.Status),
		MessagesAnalyzed: row.MessagesAnalyzed, PeriodFrom: row.PeriodFrom, PeriodTo: row.PeriodTo,
		Model: row.Model, PromptTokens: row.PromptTokens, CompletionTokens: row.CompletionTokens,
		FailureReason: row.FailureReason, StartedAt: row.StartedAt, CompletedAt: row.CompletedAt,
	}
	if len(row.Result) > 0 {
		var result uwg.Result
		if err := json.Unmarshal(row.Result, &result); err != nil {
			return nil, err
		}
		result.DropOurTeam()
		run.Result = &result
	}
	return run, nil
}

func runsToDomain(rows []schema.UnofficialWhatsAppGroupAnalysisRun) ([]uwg.Run, error) {
	out := make([]uwg.Run, 0, len(rows))
	for i := range rows {
		run, err := runToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, nil
}
