package attendance_repository

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/attendance"
)

type capturedSQL struct {
	mu         sync.Mutex
	statements []string
}

func (c *capturedSQL) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.statements...)
}

func newCapturingDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *capturedSQL) {
	t.Helper()
	captured := &capturedSQL{}
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(
		sqlmock.QueryMatcherFunc(func(_, actual string) error {
			captured.mu.Lock()
			captured.statements = append(captured.statements, actual)
			captured.mu.Unlock()
			return nil
		}),
	))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, captured
}

var castOnIndexedSide = regexp.MustCompile(`(?i)\b(l\.id|lmw\.lead_id)::text\s*=`)

func TestBacklogLeadJoinsKeepTheUuidIndexUsable(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	tenure, _ := backlogTenureQuery(scopeMessagesTable, now).build()
	reach, _ := backlogReachabilityQuery(scopeMessagesTable, "whatsapp", now).build()

	cases := map[string]struct {
		sql  string
		join string
	}{
		"tenure":           {sql: tenure, join: "l.id = NULLIF(m.lead_id, '')::uuid"},
		"tenure unknown":   {sql: backlogTenureUnknownSQL(scopeMessagesTable), join: "l.id = NULLIF(m.lead_id, '')::uuid"},
		"completeness":     {sql: backlogCompletenessSQL(scopeMessagesTable), join: "l.id = NULLIF(bl.lead_id, '')::uuid"},
		"window reachable": {sql: reach, join: "lmw.lead_id = NULLIF(m.lead_id, '')::uuid"},
	}
	for name, tc := range cases {
		if castOnIndexedSide.MatchString(tc.sql) {
			t.Fatalf("%s casts the indexed column, which forces a sequential scan:\n%s", name, tc.sql)
		}
		if !strings.Contains(tc.sql, tc.join) {
			t.Fatalf("%s does not join with %q:\n%s", name, tc.join, tc.sql)
		}
	}
}

func TestBacklogReachabilityArgumentsLineUp(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	sql, args := backlogReachabilityQuery(scopeMessagesTable, "whatsapp", now).build()
	if got := strings.Count(sql, "?"); got != len(args) {
		t.Fatalf("placeholders = %d, args = %d", got, len(args))
	}
	if args[1] != "whatsapp" {
		t.Fatalf("channel argument = %v, want whatsapp", args[1])
	}
}

func TestReopenCountsReadOnlyTheEventsTheyCount(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 24, 23, 59, 59, 0, time.UTC)
	sql, args := reopenCountsQuery("ws-1", attendance.OverviewFilter{DateFrom: &from, DateTo: &to}).build()

	if !strings.Contains(sql, "event_type IN (?, ?)") {
		t.Fatalf("reopen counts must filter on event_type so idx_conv_event_ws_type_created serves them:\n%s", sql)
	}
	if got := strings.Count(sql, "?"); got != len(args) {
		t.Fatalf("placeholders = %d, args = %d", got, len(args))
	}
	if args[3] != "reopened" || args[4] != "finished" {
		t.Fatalf("event types = %v, %v, want reopened, finished", args[3], args[4])
	}
}

func TestFRTSamplesQueryBoundsItsWindow(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	sql, args := frtSamplesQuery("ws-1", attendance.StatsFilter{DateFrom: &from}).build()
	if got := strings.Count(sql, "?"); got != len(args) {
		t.Fatalf("placeholders = %d, args = %d", got, len(args))
	}
	if !strings.Contains(sql, "ah.started_at >= ?") || strings.Contains(sql, "ah.started_at <= ?") {
		t.Fatalf("FRT window must follow the filter exactly:\n%s", sql)
	}
}

func TestScopeUsesTheSameTempTablesEveryTime(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	filter := attendance.OverviewFilter{DateFrom: &from}

	run := func() []string {
		db, mock, captured := newCapturingDB(t)
		for range 20 {
			mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 0))
		}
		if err := buildScope(db, "ws-1", filter); err != nil {
			t.Fatalf("buildScope() error = %v", err)
		}
		return captured.all()
	}

	first, second := run(), run()
	if strings.Join(first, "\n") != strings.Join(second, "\n") {
		t.Fatalf("buildScope() issued different SQL on two runs, so every request prepares new statements")
	}
	joined := strings.Join(first, "\n")
	for _, want := range []string{
		"CREATE TEMP TABLE " + scopeEntriesTable + " ON COMMIT DROP",
		"CREATE TEMP TABLE " + scopeMessagesTable + " ON COMMIT DROP",
		"ANALYZE " + scopeMessagesTable,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("buildScope() never ran %q", want)
		}
	}
}

func TestAnalyticsTransactionsAreBounded(t *testing.T) {
	db, mock, captured := newCapturingDB(t)
	mock.ExpectBegin()
	mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	repo := &repository{db: db}
	if err := repo.analyticsContext(context.Background(), func(*gorm.DB) error { return nil }); err != nil {
		t.Fatalf("analyticsContext() error = %v", err)
	}
	joined := strings.Join(captured.all(), "\n")
	if !strings.Contains(joined, "SET LOCAL statement_timeout = '"+analyticsStatementTimeout+"'") {
		t.Fatalf("analytics transactions run without a statement timeout:\n%s", joined)
	}
	if !strings.Contains(joined, "SET LOCAL jit = off") {
		t.Fatalf("analytics transactions lost jit = off:\n%s", joined)
	}
}

func TestSectionReadsOnABlankWorkspaceNeverTouchTheDatabase(t *testing.T) {
	db, _, captured := newCapturingDB(t)
	repo := &repository{db: db}
	ctx := context.Background()
	filter := attendance.OverviewFilter{}

	summary, err := repo.ReadSummary(ctx, " ", filter)
	if err != nil || len(summary.Hourly) != 24 {
		t.Fatalf("ReadSummary(blank) = %d hourly buckets, %v, want 24 and no error", len(summary.Hourly), err)
	}
	if _, err := repo.ReadTeam(ctx, "", filter); err != nil {
		t.Fatalf("ReadTeam(blank) error = %v", err)
	}
	if _, err := repo.ReadStages(ctx, "", filter); err != nil {
		t.Fatalf("ReadStages(blank) error = %v", err)
	}
	if _, err := repo.ReadBacklog(ctx, "", filter, time.Now()); err != nil {
		t.Fatalf("ReadBacklog(blank) error = %v", err)
	}
	if _, err := repo.ReadRework(ctx, "", filter); err != nil {
		t.Fatalf("ReadRework(blank) error = %v", err)
	}
	if statements := captured.all(); len(statements) != 0 {
		t.Fatalf("a blank workspace ran SQL: %v", statements)
	}
}
