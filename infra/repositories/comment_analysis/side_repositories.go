package comment_analysis_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	ca "vozko/domain/comment_analysis"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

// ---- settings ----

type settingsRepository struct{ db *gorm.DB }

func NewSettingsRepository(db *gorm.DB) ca.SettingsRepository {
	return &settingsRepository{db: db}
}

func (r *settingsRepository) Find(ctx context.Context, source ca.Source, accountID string) (*ca.Settings, error) {
	var row schema.CommentAnalysisSettings
	err := r.db.WithContext(ctx).
		Where("source = ? AND account_id = ?", string(source), accountID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrNotFound
		}
		return nil, err
	}
	return settingsToDomain(&row)
}

func (r *settingsRepository) Save(ctx context.Context, s *ca.Settings) error {
	row, err := settingsFromDomain(s)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "source"}, {Name: "account_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"workspace_id", "enabled", "model", "vertical", "topics", "severity_threshold", "daily_cap", "instructions", "updated_at",
			}),
		}).
		Create(row).Error
}

func (r *settingsRepository) ListEnabled(ctx context.Context) ([]*ca.Settings, error) {
	return r.list(r.db.WithContext(ctx).Where("enabled = true"))
}

func (r *settingsRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]*ca.Settings, error) {
	return r.list(r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("updated_at DESC"))
}

func (r *settingsRepository) list(q *gorm.DB) ([]*ca.Settings, error) {
	var rows []schema.CommentAnalysisSettings
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*ca.Settings, 0, len(rows))
	for i := range rows {
		s, err := settingsToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (r *settingsRepository) FindOverride(ctx context.Context, ref ca.ContainerRef) (*ca.ContainerOverride, error) {
	var row schema.CommentAnalysisContainerSettings
	err := r.db.WithContext(ctx).
		Where("source = ? AND account_id = ? AND container_id = ?", string(ref.Source), ref.AccountID, ref.ContainerID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrNotFound
		}
		return nil, err
	}
	return overrideToDomain(&row)
}

// SaveOverride replaces the row wholesale (PUT semantics): a nil field on
// the override is a NULL in the row, which means inherit.
func (r *settingsRepository) SaveOverride(ctx context.Context, o *ca.ContainerOverride) error {
	row, err := overrideFromDomain(o)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source"}, {Name: "account_id"}, {Name: "container_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"workspace_id", "enabled", "model", "topics", "severity_threshold", "instructions", "updated_at"}),
		}).
		Create(row).Error
}

func (r *settingsRepository) DeleteOverride(ctx context.Context, ref ca.ContainerRef) error {
	return r.db.WithContext(ctx).
		Where("source = ? AND account_id = ? AND container_id = ?", string(ref.Source), ref.AccountID, ref.ContainerID).
		Delete(&schema.CommentAnalysisContainerSettings{}).Error
}

func (r *settingsRepository) ListOverrides(ctx context.Context, source ca.Source, accountID string) ([]*ca.ContainerOverride, error) {
	var rows []schema.CommentAnalysisContainerSettings
	if err := r.db.WithContext(ctx).Where("source = ? AND account_id = ?", string(source), accountID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*ca.ContainerOverride, 0, len(rows))
	for i := range rows {
		o, err := overrideToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// ---- authors ----

type authorRepository struct{ db *gorm.DB }

func NewAuthorRepository(db *gorm.DB) ca.AuthorRepository {
	return &authorRepository{db: db}
}

// UpsertMany rebuilds counters and the derived standing but never touches
// moderation_state: the projection is recomputed, operator decisions are not.
func (r *authorRepository) UpsertMany(ctx context.Context, rows []*ca.AuthorStats) error {
	if len(rows) == 0 {
		return nil
	}
	records := make([]*schema.CommentAnalysisAuthor, 0, len(rows))
	for _, a := range rows {
		rec, err := authorFromDomain(a)
		if err != nil {
			return err
		}
		records = append(records, rec)
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "source"}, {Name: "account_id"}, {Name: "author_external_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"author_handle", "first_seen_at", "last_seen_at", "counters",
				"total_comments", "max_severity", "high_sev_count", "stance_hostile",
				"top_topics", "derived_stance", "is_flagged", "updated_at",
			}),
		}).
		CreateInBatches(records, 200).Error
}

func (r *authorRepository) FindByID(ctx context.Context, workspaceID, id string) (*ca.AuthorStats, error) {
	var row schema.CommentAnalysisAuthor
	err := r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrNotFound
		}
		return nil, err
	}
	return authorToDomain(&row)
}

func (r *authorRepository) List(ctx context.Context, in ca.AuthorsInput) (*shared.PaginatedResult[*ca.AuthorStats], error) {
	pagination := shared.NormalizePagination(in.Options.Pagination)
	q := r.db.WithContext(ctx).Model(&schema.CommentAnalysisAuthor{}).Where("workspace_id = ?", in.WorkspaceID)
	if in.Source != "" {
		q = q.Where("source = ?", string(in.Source))
	}
	if in.AccountID != "" {
		q = q.Where("account_id = ?", in.AccountID)
	}
	if in.FlaggedOnly {
		q = q.Where("is_flagged = true")
	}
	if in.Stance != "" {
		q = q.Where("derived_stance = ?", string(in.Stance))
	}
	if in.ModerationState != "" {
		q = q.Where("moderation_state = ?", string(in.ModerationState))
	}
	if in.MinComments > 0 {
		q = q.Where("total_comments >= ?", in.MinComments)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []schema.CommentAnalysisAuthor
	// The plan's order for the flagged table: worst pattern first, then the
	// single worst comment, then the loudest.
	err := q.Order("is_flagged DESC, high_sev_count DESC, max_severity DESC, total_comments DESC").
		Limit(pagination.PageSize).Offset(pagination.Offset()).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]*ca.AuthorStats, 0, len(rows))
	for i := range rows {
		a, err := authorToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *authorRepository) SetModerationState(ctx context.Context, workspaceID, id string, state ca.ModerationState, now time.Time) error {
	res := r.db.WithContext(ctx).Model(&schema.CommentAnalysisAuthor{}).
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		Updates(map[string]any{"moderation_state": string(state), "updated_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ca.ErrNotFound
	}
	return nil
}

// ---- rollups ----

type rollupRepository struct{ db *gorm.DB }

func NewRollupRepository(db *gorm.DB) ca.RollupRepository {
	return &rollupRepository{db: db}
}

func (r *rollupRepository) UpsertMany(ctx context.Context, rows []*ca.Rollup) error {
	if len(rows) == 0 {
		return nil
	}
	records := make([]*schema.CommentAnalysisRollup, 0, len(rows))
	for _, x := range rows {
		rec, err := rollupFromDomain(x)
		if err != nil {
			return err
		}
		records = append(records, rec)
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source"}, {Name: "scope"}, {Name: "scope_id"}, {Name: "bucket_date"}},
			DoUpdates: clause.AssignmentColumns([]string{"workspace_id", "account_id", "counters", "acceptance_score", "computed_at"}),
		}).
		CreateInBatches(records, 200).Error
}

func (r *rollupRepository) ListSeries(ctx context.Context, in ca.TrendInput) ([]*ca.Rollup, error) {
	var rows []schema.CommentAnalysisRollup
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND scope = ? AND scope_id = ? AND bucket_date >= ? AND bucket_date <= ?",
			in.WorkspaceID, string(in.Scope), in.ScopeID, ca.BucketDate(in.From), ca.BucketDate(in.To)).
		Order("bucket_date ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*ca.Rollup, 0, len(rows))
	for i := range rows {
		x, err := rollupToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

// ---- batches ----

type batchRepository struct{ db *gorm.DB }

func NewBatchRepository(db *gorm.DB) ca.BatchRepository {
	return &batchRepository{db: db}
}

func (r *batchRepository) Create(ctx context.Context, b *ca.Batch) error {
	return r.db.WithContext(ctx).Create(batchFromDomain(b)).Error
}

func (r *batchRepository) Totals(ctx context.Context, workspaceID string, from, to time.Time) (*ca.BatchTotals, error) {
	var t ca.BatchTotals
	err := r.db.WithContext(ctx).Model(&schema.CommentAnalysisBatch{}).
		Select(`COUNT(*) AS batches, COALESCE(SUM(item_count), 0) AS items,
			COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
			COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
			COALESCE(SUM(price_micros), 0) AS price_micros`).
		Where("workspace_id = ? AND created_at >= ? AND created_at < ?", workspaceID, from, to).
		Scan(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ---- backfills ----

type backfillRepository struct{ db *gorm.DB }

func NewBackfillRepository(db *gorm.DB) ca.BackfillRepository {
	return &backfillRepository{db: db}
}

func (r *backfillRepository) Create(ctx context.Context, b *ca.Backfill) error {
	row := backfillFromDomain(b)
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return err
	}
	b.ID = row.ID
	return nil
}

func (r *backfillRepository) Save(ctx context.Context, b *ca.Backfill) error {
	return r.db.WithContext(ctx).Model(&schema.CommentAnalysisBackfill{}).
		Where("id = ?", b.ID).
		Updates(map[string]any{
			"status":      string(b.Status),
			"cursor":      b.Cursor,
			"fetched":     b.Fetched,
			"enqueued":    b.Enqueued,
			"error":       b.Error,
			"updated_at":  b.UpdatedAt,
			"finished_at": b.FinishedAt,
		}).Error
}

func (r *backfillRepository) FindByID(ctx context.Context, workspaceID, id string) (*ca.Backfill, error) {
	var row schema.CommentAnalysisBackfill
	err := r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrNotFound
		}
		return nil, err
	}
	return backfillToDomain(&row), nil
}

// ClaimNextPending is one guarded UPDATE, like ClaimByIDs: two ticks
// cannot both run the same backfill.
func (r *backfillRepository) ClaimNextPending(ctx context.Context, now time.Time) (*ca.Backfill, error) {
	var rows []schema.CommentAnalysisBackfill
	err := r.db.WithContext(ctx).Raw(`
		UPDATE comment_analysis_backfills
		   SET status = ?, updated_at = ?
		 WHERE id = (
		       SELECT id FROM comment_analysis_backfills
		        WHERE status = ?
		        ORDER BY created_at ASC
		        LIMIT 1
		        FOR UPDATE SKIP LOCKED
		 )
		RETURNING *`,
		string(ca.BackfillRunning), now, string(ca.BackfillPending),
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return backfillToDomain(&rows[0]), nil
}

func (r *backfillRepository) FindActive(ctx context.Context, source ca.Source, accountID, containerID string) (*ca.Backfill, error) {
	var row schema.CommentAnalysisBackfill
	err := r.db.WithContext(ctx).
		Where("source = ? AND account_id = ? AND container_id = ? AND status IN ?",
			string(source), accountID, containerID, []string{string(ca.BackfillPending), string(ca.BackfillRunning)}).
		Order("created_at DESC").
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrNotFound
		}
		return nil, err
	}
	return backfillToDomain(&row), nil
}
