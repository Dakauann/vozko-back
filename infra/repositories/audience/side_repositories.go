package audience_repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

type settingsRepository struct{ db *gorm.DB }

func NewSettingsRepository(db *gorm.DB) ca.SettingsRepository {
	return &settingsRepository{db: db}
}

func (r *settingsRepository) Find(ctx context.Context, source ca.Source, accountID string) (*ca.Settings, error) {
	var row schema.AudienceSettings
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
			Columns:   []clause.Column{{Name: "source"}, {Name: "account_id"}},
			DoUpdates: clause.AssignmentColumns(settingsUpsertColumns()),
		}).
		Create(row).Error
}

func settingsUpsertColumns() []string {
	return []string{
		"workspace_id", "enabled", "model", "vertical", "topics",
		"severity_threshold", "daily_cap", "instructions",
		"reply_mode", "reply_max_auto_severity",
		"updated_at",
	}
}

func (r *settingsRepository) ListEnabled(ctx context.Context) ([]*ca.Settings, error) {
	return r.list(r.db.WithContext(ctx).Where("enabled = true"))
}

func (r *settingsRepository) ListByWorkspace(ctx context.Context, workspaceID string) ([]*ca.Settings, error) {
	return r.list(r.db.WithContext(ctx).Where("workspace_id = ?", workspaceID).Order("updated_at DESC"))
}

func (r *settingsRepository) list(q *gorm.DB) ([]*ca.Settings, error) {
	var rows []schema.AudienceSettings
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
	var row schema.AudienceContainerSettings
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
		Delete(&schema.AudienceContainerSettings{}).Error
}

func (r *settingsRepository) ListOverrides(ctx context.Context, source ca.Source, accountID string) ([]*ca.ContainerOverride, error) {
	var rows []schema.AudienceContainerSettings
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

type authorRepository struct{ db *gorm.DB }

func NewAuthorRepository(db *gorm.DB) ca.AuthorRepository {
	return &authorRepository{db: db}
}

func (r *authorRepository) UpsertMany(ctx context.Context, rows []*ca.AuthorStats) error {
	if len(rows) == 0 {
		return nil
	}
	records := make([]*schema.AudienceAuthor, 0, len(rows))
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
				"stance_supporter", "reputation",
				"top_topics", "derived_stance", "is_flagged", "updated_at",
			}),
		}).
		CreateInBatches(records, 200).Error
}

func (r *authorRepository) FindByID(ctx context.Context, workspaceID, id string) (*ca.AuthorStats, error) {
	var row schema.AudienceAuthor
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
	if in.HasPeriod() {
		return r.listAuthorsInPeriod(ctx, in)
	}
	pagination := shared.NormalizePagination(in.Options.Pagination)
	q := r.db.WithContext(ctx).Model(&schema.AudienceAuthor{}).Where("workspace_id = ?", in.WorkspaceID)
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
	if in.AuthorExternalID != "" {
		q = q.Where("author_external_id = ?", in.AuthorExternalID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []schema.AudienceAuthor
	err := q.Order(authorOrderClause(in.Sort)).
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
	res := r.db.WithContext(ctx).Model(&schema.AudienceAuthor{}).
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

type rollupRepository struct{ db *gorm.DB }

func NewRollupRepository(db *gorm.DB) ca.RollupRepository {
	return &rollupRepository{db: db}
}

func (r *rollupRepository) UpsertMany(ctx context.Context, rows []*ca.Rollup) error {
	if len(rows) == 0 {
		return nil
	}
	records := make([]*schema.AudienceRollup, 0, len(rows))
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
	var rows []schema.AudienceRollup
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

type batchRepository struct{ db *gorm.DB }

func NewBatchRepository(db *gorm.DB) ca.BatchRepository {
	return &batchRepository{db: db}
}

func (r *batchRepository) Create(ctx context.Context, b *ca.Batch) error {
	return r.db.WithContext(ctx).Create(batchFromDomain(b)).Error
}

func (r *batchRepository) Totals(ctx context.Context, workspaceID string, from, to time.Time) (*ca.BatchTotals, error) {
	type row struct {
		Kind             string
		Batches          int
		Items            int
		PromptTokens     int
		CompletionTokens int
	}
	var rows []row
	err := r.db.WithContext(ctx).Model(&schema.AudienceBatch{}).
		Select(`kind, COUNT(*) AS batches, COALESCE(SUM(item_count), 0) AS items,
			COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
			COALESCE(SUM(completion_tokens), 0) AS completion_tokens`).
		Where("workspace_id = ? AND created_at >= ? AND created_at < ?", workspaceID, from, to).
		Group("kind").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	t := ca.BatchTotals{ByKind: map[ca.BatchKind]ca.BatchTotals{}}
	for _, x := range rows {
		part := ca.BatchTotals{
			Batches: x.Batches, Items: x.Items,
			PromptTokens: x.PromptTokens, CompletionTokens: x.CompletionTokens,
		}
		t.Batches += part.Batches
		t.Items += part.Items
		t.PromptTokens += part.PromptTokens
		t.CompletionTokens += part.CompletionTokens
		t.ByKind[ca.NormalizeBatchKind(ca.BatchKind(x.Kind))] = part
	}
	return &t, nil
}

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
	return r.db.WithContext(ctx).Model(&schema.AudienceBackfill{}).
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
	var row schema.AudienceBackfill
	err := r.db.WithContext(ctx).Where("workspace_id = ? AND id = ?", workspaceID, id).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ca.ErrNotFound
		}
		return nil, err
	}
	return backfillToDomain(&row), nil
}

func (r *backfillRepository) ClaimNextPending(ctx context.Context, now time.Time) (*ca.Backfill, error) {
	var rows []schema.AudienceBackfill
	err := r.db.WithContext(ctx).Raw(`
		UPDATE audience_backfills
		   SET status = ?, updated_at = ?
		 WHERE id = (
		       SELECT id FROM audience_backfills
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
	var row schema.AudienceBackfill
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

func authorOrderClause(sort ca.Sort) string {
	dir := "DESC"
	if sort.Ascending {
		dir = "ASC"
	}

	switch sort.Key {
	case ca.SortAuthorReputation:
		return "reputation " + dir + ", id ASC"
	case ca.SortAuthorComments:
		return "total_comments " + dir + ", id ASC"
	case ca.SortAuthorNegative:
		return "stance_hostile " + dir + ", high_sev_count " + dir + ", id ASC"
	case ca.SortAuthorPositive:
		return "stance_supporter " + dir + ", id ASC"
	case ca.SortAuthorSeverity:
		return "high_sev_count " + dir + ", max_severity " + dir + ", id ASC"
	case ca.SortAuthorLastSeen:
		return "last_seen_at " + dir + ", id ASC"
	case ca.SortAuthorFirstSeen:
		return "first_seen_at " + dir + ", id ASC"
	}

	return "reputation ASC, id ASC"
}

func (r *authorRepository) SetRole(ctx context.Context, workspaceID, id string, role ca.AuthorRoleInference, now time.Time) error {
	role.Normalize()
	res := r.db.WithContext(ctx).Model(&schema.AudienceAuthor{}).
		Where("workspace_id = ? AND id = ?", workspaceID, id).
		Updates(map[string]any{
			"role":            string(role.Role),
			"role_confidence": string(role.Confidence),
			"role_comments":   role.BasedOnComments,
			"role_rationale":  role.Rationale,
			"updated_at":      now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ca.ErrNotFound
	}
	return nil
}

func (r *authorRepository) ListForRoleInference(ctx context.Context, source ca.Source, accountID string, minComments, limit int) ([]*ca.AuthorStats, error) {
	var rows []schema.AudienceAuthor
	err := r.db.WithContext(ctx).
		Where("source = ? AND account_id = ? AND total_comments >= ?", string(source), accountID, minComments).
		Order("total_comments DESC, id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]*ca.AuthorStats, 0, len(rows))
	for i := range rows {
		a, err := authorToDomain(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
