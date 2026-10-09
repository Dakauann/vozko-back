package lead

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

const (
	pageWorkspace = "8f2d6a4e-1c3b-4d5e-9f7a-0b1c2d3e4f5a"
	firstLead     = "0b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"
	secondLead    = "1b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"
)

func pageColumns() []string {
	return []string{
		"id", "workspace_id", "number", "name", "profile_picture_url", "age", "blocked", "blocked_at", "blocked_by",
		"owner_id", "owner_kind", "custom_fields", "relatives_count", "referred_count", "version", "created_at", "updated_at",
		"campaign_count", "memory_count", "last_activity_at", "last_memory_at", "window_open", "window_expires_at",
	}
}

func pageRow(rows *sqlmock.Rows, id string) *sqlmock.Rows {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	return rows.AddRow(id, pageWorkspace, "5511987654321", "Ana", "", nil, false, nil, nil,
		nil, nil, nil, 2, 1, int64(3), now, now,
		4, 1, now, nil, false, nil)
}

func TestListWithSummary_ReadsThePageIdsFirstThenSummarisesOnlyThatPage(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM leads WHERE leads.workspace_id = $1 AND leads.deleted_at IS NULL")).
		WithArgs(pageWorkspace).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(450))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT leads.id FROM leads WHERE leads.workspace_id = $1 AND leads.deleted_at IS NULL ORDER BY leads.relatives_count DESC NULLS LAST, leads.id DESC LIMIT $2 OFFSET $3")).
		WithArgs(pageWorkspace, 200, 200).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(secondLead).AddRow(firstLead))
	mock.ExpectQuery(`^SELECT leads\.id, leads\.workspace_id, .* FROM leads LEFT JOIN LATERAL \(SELECT .* AS campaign_count, .* AS window_expires_at\) summary ON true WHERE leads\.workspace_id = \$1 AND leads\.id = ANY\(\$2::uuid\[\]\)$`).
		WithArgs(pageWorkspace, sqlmock.AnyArg()).
		WillReturnRows(pageRow(pageRow(sqlmock.NewRows(pageColumns()), firstLead), secondLead))

	page, err := r.ListWithSummary(lead.ListLeadsInput{
		WorkspaceID: pageWorkspace,
		Options: shared.QueryOptions{
			Pagination: shared.Pagination{Page: 2, PageSize: 200},
			Sorts:      []shared.Sort{{Field: string(lead.SortRelatives), Direction: shared.SortDesc}},
		},
	})
	if err != nil {
		t.Fatalf("ListWithSummary() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if page.PageSize != 200 || page.TotalPages != 3 || page.TotalItems != 450 {
		t.Fatalf("page = size %d, pages %d, total %d; want 200, 3, 450", page.PageSize, page.TotalPages, page.TotalItems)
	}
	if len(page.Items) != 2 || page.Items[0].Lead.ID != secondLead || page.Items[1].Lead.ID != firstLead {
		t.Fatalf("items must keep the order of the id read, got %v", page.Items)
	}
	if page.Items[0].Summary.TotalCampaigns != 4 || page.Items[0].Lead.RelativesCount != 2 {
		t.Fatalf("summary = %+v, lead = %+v", page.Items[0].Summary, page.Items[0].Lead)
	}
}

func TestListWithSummary_AnEmptyPageReadsNoSummaries(t *testing.T) {
	db, mock, _ := newMockDB(t)
	r := &repository{db: db, agg: newAggregateCache(nil)}
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM leads`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT leads\.id FROM leads WHERE`).WillReturnRows(sqlmock.NewRows([]string{"id"}))

	page, err := r.ListWithSummary(lead.ListLeadsInput{WorkspaceID: pageWorkspace})
	if err != nil {
		t.Fatalf("ListWithSummary() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("items = %v, want none", page.Items)
	}
}

func TestPageQueriesBindOnePlaceholderPerArgument(t *testing.T) {
	r := newNilRepo()
	q, err := r.compile(lead.ListLeadsInput{
		WorkspaceID: pageWorkspace,
		Today:       time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC),
		Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldQuery, Operator: crmfilter.OpContains, Values: []string{"ana"}},
			{Field: crmfilter.FieldDistrict, Operator: crmfilter.OpIn, Values: []string{"sp:sao paulo/centro"}},
			{Field: crmfilter.FieldBirthday, Operator: crmfilter.OpEquals, Values: []string{crmfilter.BirthdayToday}},
		}}}},
	})
	if err != nil {
		t.Fatalf("compile() error = %v", err)
	}
	ids, idArgs := q.pageIDs(shared.QueryOptions{Sorts: []shared.Sort{{Field: string(lead.SortLastActivityAt), Direction: shared.SortDesc}}})
	if got := strings.Count(ids, "?"); got != len(idArgs) {
		t.Fatalf("id read has %d placeholders for %d args: %s", got, len(idArgs), ids)
	}
	if !strings.Contains(ids, "ORDER BY GREATEST(") {
		t.Fatalf("a computed sort must order by its expression in the id read, got %s", ids)
	}
	summaries, sumArgs := q.pageSummaries([]string{firstLead})
	if got := strings.Count(summaries, "?"); got != len(sumArgs) {
		t.Fatalf("summary read has %d placeholders for %d args: %s", got, len(sumArgs), summaries)
	}
}

func TestCompileKeepsTheFilterSentinel(t *testing.T) {
	_, err := newNilRepo().compile(lead.ListLeadsInput{
		WorkspaceID: pageWorkspace,
		Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldGeoStatus, Operator: crmfilter.OpEquals, Values: []string{"lost"}},
		}}}},
	})
	if !errors.Is(err, lead.ErrLeadFilterInvalid) || !errors.Is(err, crmfilter.ErrInvalidValue) {
		t.Fatalf("compile() = %v, want both ErrLeadFilterInvalid and the crmfilter sentinel", err)
	}

	_, err = newNilRepo().compile(lead.ListLeadsInput{
		WorkspaceID: pageWorkspace,
		Filter: crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{
			{Field: crmfilter.FieldPipeline, Operator: crmfilter.OpIn, Values: []string{"p"}},
		}}}},
	})
	if !errors.Is(err, lead.ErrLeadFilterInvalid) || !errors.Is(err, crmfilter.ErrNotApplicable) {
		t.Fatalf("compile() = %v, want the not-applicable sentinel kept", err)
	}
}
