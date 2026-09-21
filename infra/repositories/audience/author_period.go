package audience_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	ca "vozko/domain/audience"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
)

const maxPeriodAuthorCandidates = 5000

type periodAuthorRow struct {
	CountersRow

	AuthorID         string
	WorkspaceID      string
	Source           string
	AccountID        string
	AuthorExternalID string
	AuthorHandle     string
	FirstSeenAt      time.Time
	LastSeenAt       time.Time

	ModerationState string
	Role            string
	RoleConfidence  string
	RoleComments    int
	RoleRationale   string
}

func (r *authorRepository) listAuthorsInPeriod(ctx context.Context, in ca.AuthorsInput) (*shared.PaginatedResult[*ca.AuthorStats], error) {
	pagination := shared.NormalizePagination(in.Options.Pagination)
	derivedFilter := in.FlaggedOnly || in.Stance != ""

	limit, offset := pagination.PageSize, pagination.Offset()
	if derivedFilter {
		limit, offset = maxPeriodAuthorCandidates, 0
	}

	var rows []periodAuthorRow
	err := r.periodQuery(ctx, in).
		Select(periodAuthorSelect).
		Group("audience_analyses.author_external_id, a.id, a.moderation_state, a.role, a.role_confidence, a.role_comments, a.role_rationale").
		Having(periodHaving(in)).
		Order(periodAuthorOrderClause(in.Sort)).
		Limit(limit).Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	items := make([]*ca.AuthorStats, 0, len(rows))
	for i := range rows {
		items = append(items, rows[i].toDomain())
	}

	if !derivedFilter {
		total, err := r.countAuthorsInPeriod(ctx, in)
		if err != nil {
			return nil, err
		}
		return shared.NewPaginatedResult(items, pagination, total), nil
	}

	kept := make([]*ca.AuthorStats, 0, len(items))
	for _, a := range items {
		if in.FlaggedOnly && !a.IsFlagged {
			continue
		}
		if in.Stance != "" && a.DerivedStance != in.Stance {
			continue
		}
		kept = append(kept, a)
	}
	total := int64(len(kept))

	start := pagination.Offset()
	if start > len(kept) {
		start = len(kept)
	}
	end := start + pagination.PageSize
	if end > len(kept) {
		end = len(kept)
	}
	return shared.NewPaginatedResult(kept[start:end], pagination, total), nil
}

func (r *authorRepository) countAuthorsInPeriod(ctx context.Context, in ca.AuthorsInput) (int64, error) {
	if in.MinComments > 0 {
		var counted []struct{ N int64 }
		err := r.periodQuery(ctx, in).
			Select("COUNT(*) AS n").
			Group("audience_analyses.author_external_id").
			Having(periodHaving(in)).
			Scan(&counted).Error
		if err != nil {
			return 0, err
		}
		return int64(len(counted)), nil
	}
	var total int64
	err := r.periodQuery(ctx, in).
		Distinct("audience_analyses.author_external_id").
		Count(&total).Error
	return total, err
}

func (r *authorRepository) periodQuery(ctx context.Context, in ca.AuthorsInput) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&schema.AudienceAnalysis{}).
		Joins(`LEFT JOIN audience_authors a
			ON a.source = audience_analyses.source
			AND a.account_id = audience_analyses.account_id
			AND a.author_external_id = audience_analyses.author_external_id`).
		Where("audience_analyses.workspace_id = ? AND audience_analyses.deleted_at IS NULL", in.WorkspaceID).
		Where("audience_analyses.author_external_id <> ''")

	if in.Source != "" {
		q = q.Where("audience_analyses.source = ?", string(in.Source))
	}
	if in.AccountID != "" {
		q = q.Where("audience_analyses.account_id = ?", in.AccountID)
	}
	if in.AuthorExternalID != "" {
		q = q.Where("audience_analyses.author_external_id = ?", in.AuthorExternalID)
	}
	if in.From != nil {
		q = q.Where("audience_analyses.occurred_at >= ?", *in.From)
	}
	if in.To != nil {
		q = q.Where("audience_analyses.occurred_at < ?", *in.To)
	}
	if in.ModerationState != "" {
		q = q.Where("COALESCE(a.moderation_state, 'none') = ?", string(in.ModerationState))
	}
	return q
}

func periodHaving(in ca.AuthorsInput) string {
	if in.MinComments > 0 {
		return "COUNT(*) >= " + itoa(in.MinComments)
	}
	return ""
}

var periodAuthorSelect = `
	audience_analyses.author_external_id AS author_external_id,
	(ARRAY_AGG(audience_analyses.author_handle ORDER BY audience_analyses.occurred_at DESC))[1] AS author_handle,
	MIN(audience_analyses.workspace_id::text) AS workspace_id,
	MIN(audience_analyses.source) AS source,
	MIN(audience_analyses.account_id::text) AS account_id,
	MIN(audience_analyses.occurred_at) AS first_seen_at,
	MAX(audience_analyses.occurred_at) AS last_seen_at,
	a.id AS author_id,
	COALESCE(a.moderation_state, 'none') AS moderation_state,
	COALESCE(a.role, 'unknown') AS role,
	COALESCE(a.role_confidence, 'none') AS role_confidence,
	COALESCE(a.role_comments, 0) AS role_comments,
	COALESCE(a.role_rationale, '') AS role_rationale,
` + countersSelect

func periodAuthorOrderClause(s ca.Sort) string {
	dir := "DESC"
	if s.Ascending {
		dir = "ASC"
	}
	const analysed = "status = 'analyzed'"
	var (
		supporters = "COUNT(*) FILTER (WHERE " + analysed + " AND stance = 'supporter')"
		critics    = "COUNT(*) FILTER (WHERE " + analysed + " AND stance = 'critic')"
		hostiles   = "COUNT(*) FILTER (WHERE " + analysed + " AND stance = 'hostile')"
		highSev    = "COUNT(*) FILTER (WHERE " + analysed + " AND severity >= 60)"
		maxSev     = "COALESCE(MAX(severity) FILTER (WHERE " + analysed + "), 0)"
		reputation = "(" + supporters + " * 2 - " + critics + " - " + hostiles + " * 2 - " + highSev + " * 2)"
	)

	tail := ", a.id ASC, audience_analyses.author_external_id ASC"
	switch s.Key {
	case ca.SortAuthorComments:
		return "COUNT(*) " + dir + tail
	case ca.SortAuthorNegative:
		return hostiles + " " + dir + ", " + highSev + " " + dir + tail
	case ca.SortAuthorPositive:
		return supporters + " " + dir + tail
	case ca.SortAuthorSeverity:
		return highSev + " " + dir + ", " + maxSev + " " + dir + tail
	case ca.SortAuthorLastSeen:
		return "MAX(audience_analyses.occurred_at) " + dir + tail
	case ca.SortAuthorFirstSeen:
		return "MIN(audience_analyses.occurred_at) " + dir + tail
	}
	return reputation + " " + dir + tail
}

func (row periodAuthorRow) toDomain() *ca.AuthorStats {
	a := &ca.AuthorStats{
		ID:               row.AuthorID,
		WorkspaceID:      row.WorkspaceID,
		Source:           ca.Source(row.Source),
		AccountID:        row.AccountID,
		AuthorExternalID: row.AuthorExternalID,
		AuthorHandle:     row.AuthorHandle,
		FirstSeenAt:      row.FirstSeenAt,
		LastSeenAt:       row.LastSeenAt,
		Counters:         row.CountersRow.counters(),
		TopTopics:        []ca.TopicCount{},
		ModerationState:  ca.ModerationState(strings.TrimSpace(row.ModerationState)),
		Role: ca.AuthorRoleInference{
			Role:            ca.AuthorRole(row.Role),
			Confidence:      shared.QualityLevel(row.RoleConfidence),
			BasedOnComments: row.RoleComments,
			Rationale:       row.RoleRationale,
		},
		UpdatedAt: row.LastSeenAt,
	}
	a.Derive()
	return a
}

func itoa(n int) string {
	if n <= 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
