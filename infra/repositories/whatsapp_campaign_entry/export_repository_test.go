package whatsapp_campaign_entry

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/export"
)

// capturedQueries records every SQL statement the repository issues, so tests
// can assert on the shape of the query rather than only on its results. The
// predicates below are tenancy and cost controls; a result-only test would pass
// just as happily if they were dropped.
type capturedQueries struct{ sql []string }

func (c *capturedQueries) matcher() sqlmock.QueryMatcher {
	return sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		c.sql = append(c.sql, actualSQL)
		return nil
	})
}

func newExportMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *capturedQueries, *sql.DB) {
	t.Helper()
	captured := &capturedQueries{}
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captured.matcher()))
	if err != nil {
		t.Fatal(err)
	}
	mock.MatchExpectationsInOrder(true)

	db, err := gorm.Open(
		postgres.New(postgres.Config{
			Conn:                 sqlDB,
			PreferSimpleProtocol: true,
			WithoutReturning:     true,
		}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, captured, sqlDB
}

func exportRows(n int, campaignID string) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{
		"entry_id", "campaign_id", "campaign_name", "status",
		"created_at", "updated_at", "variables", "metadata",
		"lead_number", "lead_name", "lead_age",
	})
	base := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		rows.AddRow(
			"entry-"+itoa(i), campaignID, "Campanha A", "DELIVERED",
			base.Add(time.Duration(i)*time.Second), base.Add(time.Duration(i)*time.Second),
			"{}", []byte(`{}`),
			"5511900000001", "Ana", nil,
		)
	}
	return rows
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func collect(t *testing.T, repo export.ChannelEntryLister, scope export.Scope) []export.ChannelEntry {
	t.Helper()
	var out []export.ChannelEntry
	err := repo.ListForExport(context.Background(), scope, func(e export.ChannelEntry) error {
		out = append(out, e)
		return nil
	})
	if err != nil {
		t.Fatalf("list for export: %v", err)
	}
	return out
}

// Tenancy is enforced in this query and nowhere else. The handler checks the
// workspace for the per-campaign route, but the workspace-wide route has no id
// to check — so if the predicate is not here, an export reads every tenant.
func TestExportQueryScopesToTheWorkspace(t *testing.T) {
	db, mock, captured, sqlDB := newExportMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(".").WillReturnRows(exportRows(2, "camp-1"))

	repo := NewExportRepository(db)
	collect(t, repo, export.Scope{WorkspaceID: "ws-1"})

	if len(captured.sql) != 1 {
		t.Fatalf("issued %d queries, want 1", len(captured.sql))
	}
	q := captured.sql[0]
	for _, want := range []string{
		"c.workspace_id",
		"e.deleted_at IS NULL",
		"c.deleted_at IS NULL",
		"l.workspace_id = c.workspace_id",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query is missing %q:\n%s", want, q)
		}
	}
}

func TestExportQueryAppliesEveryScopeFilter(t *testing.T) {
	db, mock, captured, sqlDB := newExportMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(".").WillReturnRows(exportRows(1, "camp-1"))

	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC)

	repo := NewExportRepository(db)
	collect(t, repo, export.Scope{
		WorkspaceID:   "ws-1",
		ContainerID:   "camp-1",
		ContainerType: "standard",
		DepartmentIDs: []string{"dep-1", "dep-2"},
		Statuses:      []string{"SENT", "DELIVERED", "READ"},
		CreatedFrom:   &from,
		CreatedTo:     &to,
	})

	q := captured.sql[0]
	for _, want := range []string{
		"e.campaign_id",
		"c.type",
		"e.status IN",
		"c.created_at >=",
		"c.created_at <=",
		"c.department_id IN",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query is missing %q:\n%s", want, q)
		}
	}
}

// An empty scope must not silently mean "every workspace".
func TestExportWithoutWorkspaceQueriesNothing(t *testing.T) {
	db, _, captured, sqlDB := newExportMockDB(t)
	defer sqlDB.Close()

	repo := NewExportRepository(db)
	got := collect(t, repo, export.Scope{})

	if len(got) != 0 {
		t.Errorf("emitted %d rows without a workspace", len(got))
	}
	if len(captured.sql) != 0 {
		t.Errorf("issued %d queries without a workspace", len(captured.sql))
	}
}

// Paging is keyset, not OFFSET: each page carries the last row's ordering tuple
// so the database resumes on the index instead of re-reading and re-sorting
// everything before it. With OFFSET, page N costs N pages of work, which is
// what turns a large export into a slow query for every other tenant sharing
// the pool.
func TestExportPagesWithAKeysetCursor(t *testing.T) {
	db, mock, captured, sqlDB := newExportMockDB(t)
	defer sqlDB.Close()

	// A full page forces a second round trip; a short page ends the walk.
	mock.ExpectQuery(".").WillReturnRows(exportRows(exportPageSize, "camp-1"))
	mock.ExpectQuery(".").WillReturnRows(exportRows(3, "camp-2"))

	repo := NewExportRepository(db)
	got := collect(t, repo, export.Scope{WorkspaceID: "ws-1"})

	if len(got) != exportPageSize+3 {
		t.Fatalf("emitted %d rows, want %d", len(got), exportPageSize+3)
	}
	if len(captured.sql) != 2 {
		t.Fatalf("issued %d queries, want 2", len(captured.sql))
	}

	first, second := captured.sql[0], captured.sql[1]
	if strings.Contains(first, "OFFSET") || strings.Contains(second, "OFFSET") {
		t.Error("export pages with OFFSET; it must page with a keyset cursor")
	}
	if strings.Contains(first, "e.campaign_id, e.status, e.created_at, e.id) >") {
		t.Error("first page carries a cursor predicate")
	}
	if !strings.Contains(second, "e.campaign_id, e.status, e.created_at, e.id) >") {
		t.Errorf("second page is missing the keyset predicate:\n%s", second)
	}

	// The ordering has to be the index's leading edge, or every page pays for a
	// sort of the whole filtered set.
	if !strings.Contains(first, "ORDER BY e.campaign_id, e.status, e.created_at, e.id") {
		t.Errorf("unexpected ordering:\n%s", first)
	}
}

func TestExportStopsWhenTheCallerStops(t *testing.T) {
	db, mock, _, sqlDB := newExportMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(".").WillReturnRows(exportRows(exportPageSize, "camp-1"))

	repo := NewExportRepository(db)

	stop := context.Canceled
	seen := 0
	err := repo.ListForExport(context.Background(), export.Scope{WorkspaceID: "ws-1"},
		func(export.ChannelEntry) error {
			seen++
			return stop
		})

	if err != stop {
		t.Fatalf("err = %v, want the emit error unchanged", err)
	}
	if seen != 1 {
		t.Errorf("kept emitting after the caller stopped: %d rows", seen)
	}
}

func TestExportCarriesTheCampaignNameAndLeadIdentity(t *testing.T) {
	db, mock, _, sqlDB := newExportMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(".").WillReturnRows(exportRows(1, "camp-1"))

	repo := NewExportRepository(db)
	got := collect(t, repo, export.Scope{WorkspaceID: "ws-1"})

	if len(got) != 1 {
		t.Fatalf("emitted %d rows", len(got))
	}
	e := got[0]
	if e.ContainerName != "Campanha A" {
		t.Errorf("ContainerName = %q", e.ContainerName)
	}
	if e.Number != "5511900000001" || e.Name != "Ana" {
		t.Errorf("lead identity = %q/%q", e.Number, e.Name)
	}
	if e.Status != "DELIVERED" {
		t.Errorf("Status = %q", e.Status)
	}
	if e.CreatedAt == "" || e.UpdatedAt == "" {
		t.Error("timestamps were not formatted")
	}
}

func TestExportHonoursACancelledContext(t *testing.T) {
	db, _, captured, sqlDB := newExportMockDB(t)
	defer sqlDB.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repo := NewExportRepository(db)
	err := repo.ListForExport(ctx, export.Scope{WorkspaceID: "ws-1"}, func(export.ChannelEntry) error {
		t.Error("emitted a row for a cancelled export")
		return nil
	})

	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(captured.sql) != 0 {
		t.Errorf("queried the database for a cancelled export")
	}
}
