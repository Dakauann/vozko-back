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

// The ranking over a WINDOW.
//
// The lifetime ranking reads audience_authors, a projection with one
// row per person and no time dimension at all. So "quem mais me atacou esta
// semana" cannot be answered by adding a predicate to it: the row simply does
// not hold that. This regroups the comments themselves for the window and
// derives the standing from the result, exactly as the rollup does when it
// rebuilds the projection.
//
// Two things are joined back from the projection rather than recomputed,
// because they are not functions of the comments in any window: the author's
// row id (which every other endpoint addresses them by) and the two things a
// person or a model decided about them, moderation state and inferred role.
//
// Division of labour, and the reason this file is shaped the way it is:
// SQL COUNTS, Go DERIVES. Severity thresholds, the stance weights and the
// flagging rule live in the domain and are not restated in SQL. The one
// exception is the ORDER BY for the reputation key, which needs an expression
// monotonic with the domain's formula; see periodAuthorOrderClause.

// maxPeriodAuthorCandidates bounds the rows a windowed ranking pulls back
// before deriving.
//
// It only binds when a DERIVED filter is active (stance, flagged), because
// those cannot be expressed in SQL without restating domain rules. The rows
// come back already ordered by the requested key, so the cap keeps exactly the
// ones the reader is looking at. Generous enough that no real account reaches
// it: a busy political account sees low thousands of distinct commenters a
// month.
const maxPeriodAuthorCandidates = 5000

// periodAuthorRow is one grouped author, before the domain derives anything.
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

// listAuthorsInPeriod answers the windowed ranking.
func (r *authorRepository) listAuthorsInPeriod(ctx context.Context, in ca.AuthorsInput) (*shared.PaginatedResult[*ca.AuthorStats], error) {
	pagination := shared.NormalizePagination(in.Options.Pagination)
	derivedFilter := in.FlaggedOnly || in.Stance != ""

	// Without a derived filter the count and the page are both exact in SQL.
	// With one, the filtering happens in Go, so the page is cut there too.
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

	// Derived filters: the domain has now computed the standing for THIS
	// window, so the predicate is applied to that rather than to a SQL
	// expression restating the same rule in a second place.
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

// countAuthorsInPeriod counts DISTINCT people, not comments. Counting the
// grouped rows would return the number of comments and hand the table a page
// count it cannot fill.
func (r *authorRepository) countAuthorsInPeriod(ctx context.Context, in ca.AuthorsInput) (int64, error) {
	if in.MinComments > 0 {
		// With a HAVING clause the count has to run over the groups, so it
		// wraps the grouped query rather than counting distinct ids.
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

// periodQuery is the shared FROM and WHERE: one workspace's live comments in
// the window, with the author projection joined for the identity and the two
// operator-owned fields.
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
	// Moderation state is operator-owned and lives on the joined row, so it
	// filters like an ordinary column rather than being derived.
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

// periodAuthorSelect groups the counters with countersSelect, the same set the
// lifetime projection is built from, so the two paths cannot count differently.
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

// periodAuthorOrderClause maps a sort key to the grouped expression, mirroring
// authorOrderClause on the lifetime path.
//
// The reputation key is the one place a domain formula is echoed in SQL, and it
// is echoed WITHOUT its rounding: the aggregate is scaled by two so it stays an
// integer, and ordering by the unrounded points is equivalent to ordering by
// the rounded ones because the domain's rounding is monotonic. The number the
// customer SEES is still AuthorReputation's, computed in Go from the same
// counts. An integration test pins the two orders together.
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

	// The SAME tiebreak the lifetime path uses, or two people with identical
	// counts come back in one order from one query and the other order from
	// the other, and the wide-window parity test catches it. The external id is
	// appended for the rows a rollup has not projected yet, whose a.id is NULL:
	// without it those tie on NULL and paging over them is not stable.
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

// toDomain hands the counts to the domain and lets it decide what they mean.
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
	// The standing is for THIS window: the stance, the flag and the reputation
	// all describe how the person behaved inside it, which is the whole point
	// of asking for a window.
	a.Derive()
	return a
}

// itoa avoids a strconv import for the one small integer that reaches SQL, and
// it is bounded by Validate before it gets here.
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
