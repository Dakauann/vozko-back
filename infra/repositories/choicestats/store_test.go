package choicestats_repository

import (
	"context"
	"database/sql/driver"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/choicestats"
)

func mockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func sqlPattern(query string) string {
	return "^" + strings.ReplaceAll(regexp.QuoteMeta(query), `\?`, `\$[0-9]+`) + "$"
}

func sendRecord(t *testing.T) choicestats.SendRecord {
	t.Helper()
	rec, err := choicestats.Send{
		WorkspaceID: "8f6c2a52-1b0f-4f7e-9a34-0d5f8f1e2c3b", PromptKey: "wf:run-1:ask#2", ProviderMessageID: "wamid.1",
		Source: choicestats.Source{Kind: choicestats.SourceWorkflow, ID: "wf-1", NodeID: "ask", Version: 4},
		Shown:  []string{"sim", "nao"}, SentAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
	}.Record(time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestASendInsertsItsPromptAndCountsOnlyWhenTheKeyIsNew(t *testing.T) {
	if got := strings.Count(recordSendSQL, "?"); got != 12 {
		t.Fatalf("placeholders = %d, want 12", got)
	}
	for _, fragment := range []string{"ON CONFLICT (prompt_key) DO NOTHING", "FROM inserted i CROSS JOIN unnest(?::text[])", "shown = choice_daily_counts.shown + 1"} {
		if !strings.Contains(recordSendSQL, fragment) {
			t.Fatalf("statement misses %q", fragment)
		}
	}
	rec := sendRecord(t)
	db, mock := mockDB(t)
	mock.ExpectExec(sqlPattern(recordSendSQL)).
		WithArgs(rec.PromptKey, rec.WorkspaceID, "workflow", "wf-1", "ask", 4, "", "wamid.1", rec.SentDay, rec.SentAt, rec.Shard,
			pq.Array([]string{"", "sim", "nao"})).
		WillReturnResult(sqlmock.NewResult(0, 3))
	isNew, err := New(db).RecordSend(context.Background(), rec)
	if err != nil || !isNew {
		t.Fatalf("new = %v, err = %v", isNew, err)
	}

	mock.ExpectExec(sqlPattern(recordSendSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
	isNew, err = New(db).RecordSend(context.Background(), rec)
	if err != nil || isNew {
		t.Fatalf("a repeated key counted again: new = %v, err = %v", isNew, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAClickUpdatesAnUnclickedPromptInsideTheWindowThenCounts(t *testing.T) {
	at := time.Date(2026, 10, 22, 13, 0, 0, 0, time.UTC)
	byKey, err := choicestats.Click{WorkspaceID: "ws", PromptKey: "wf:run-1:ask#2", Option: "sim", At: at}.Record()
	if err != nil {
		t.Fatal(err)
	}
	byMessage, err := choicestats.Click{WorkspaceID: "ws", ProviderMessageID: "wamid.1", Option: "template_button:0", At: at}.Record()
	if err != nil {
		t.Fatal(err)
	}
	freeText, err := choicestats.Click{WorkspaceID: "ws", PromptKey: "wf:run-1:ask#2", Option: choicestats.OptionFreeText, At: at}.Record()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		click choicestats.ClickRecord
		sql   string
		args  []driver.Value
	}{
		{"by key", byKey, clickSQL(clickByKey, true), []driver.Value{"sim", at, "ws", "wf:run-1:ask#2", byKey.NotBefore, "sim", pq.Array([]string{"sim", ""})}},
		{"by message", byMessage, clickSQL(clickByMessage, true), []driver.Value{"template_button:0", at, "ws", "wamid.1", byMessage.NotBefore, "template_button:0", pq.Array([]string{"template_button:0", ""})}},
		{"free text needs no shown option", freeText, clickSQL(clickByKey, false), []driver.Value{"no_match", at, "ws", "wf:run-1:ask#2", freeText.NotBefore, pq.Array([]string{"no_match"})}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Count(tc.sql, "?"); got != len(tc.args) {
				t.Fatalf("placeholders = %d, args = %d", got, len(tc.args))
			}
			for _, fragment := range []string{"p.clicked_option_id = ''", "p.sent_at >= ?", "clicks = choice_daily_counts.clicks + 1"} {
				if !strings.Contains(tc.sql, fragment) {
					t.Fatalf("statement misses %q", fragment)
				}
			}
			db, mock := mockDB(t)
			mock.ExpectExec(sqlPattern(tc.sql)).WithArgs(tc.args...).WillReturnResult(sqlmock.NewResult(0, 2))
			counted, err := New(db).RecordClick(context.Background(), tc.click)
			if err != nil || !counted {
				t.Fatalf("counted = %v, err = %v", counted, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	if !strings.Contains(clickSQL(clickByKey, true), "c.option_id = ? AND c.day = p.sent_day AND c.shard = p.shard") {
		t.Fatal("an option click must name a shown option of the same cohort")
	}
}

func TestCountsReadTheSourceOrTheTemplateWithinTheDays(t *testing.T) {
	from := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
	version := 3
	cases := []struct {
		name  string
		query choicestats.CountQuery
		sql   string
		args  []driver.Value
	}{
		{"all versions", choicestats.CountQuery{WorkspaceID: "ws", Kind: choicestats.SourceWorkflow, SourceID: "wf-1", From: from, To: to},
			sourceCountsSQL(false), []driver.Value{"ws", "workflow", "wf-1", from, to}},
		{"one version", choicestats.CountQuery{WorkspaceID: "ws", Kind: choicestats.SourceWorkflow, SourceID: "wf-1", Version: &version, From: from, To: to},
			sourceCountsSQL(true), []driver.Value{"ws", "workflow", "wf-1", from, to, 3}},
		{"a template", choicestats.CountQuery{WorkspaceID: "ws", TemplateID: "tpl-1", From: from, To: to},
			templateCountsSQL, []driver.Value{"ws", "tpl-1", from, to}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Count(tc.sql, "?"); got != len(tc.args) {
				t.Fatalf("placeholders = %d, args = %d", got, len(tc.args))
			}
			db, mock := mockDB(t)
			mock.ExpectQuery(sqlPattern(tc.sql)).WithArgs(tc.args...).WillReturnRows(
				sqlmock.NewRows([]string{"node_id", "option_id", "shown", "clicks"}).AddRow("ask", "", 10, 4).AddRow("ask", "sim", 10, 4))
			rows, err := New(db).Counts(context.Background(), tc.query)
			if err != nil || len(rows) != 2 || rows[1].OptionID != "sim" || rows[1].Clicks != 4 {
				t.Fatalf("rows = %+v, err = %v", rows, err)
			}
		})
	}
	db, _ := mockDB(t)
	if _, err := New(db).Counts(context.Background(), choicestats.CountQuery{WorkspaceID: "ws", From: from, To: to}); err == nil {
		t.Fatal("a query naming neither a source nor a template was run")
	}
}

func TestPruningDeletesInBoundedBatches(t *testing.T) {
	cutoff := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		sql  string
		run  func(*Store) (int64, error)
	}{
		{"prompts past the attribution window", prunePromptsSQL, func(s *Store) (int64, error) { return s.PrunePrompts(cutoff, 5000) }},
		{"counters past the retention", pruneCountsSQL, func(s *Store) (int64, error) { return s.PruneCounts(cutoff, 5000) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Count(tc.sql, "?"); got != 2 || !strings.Contains(tc.sql, "LIMIT ?") {
				t.Fatalf("an unbounded delete:\n%s", tc.sql)
			}
			db, mock := mockDB(t)
			mock.ExpectExec(sqlPattern(tc.sql)).WithArgs(cutoff, 5000).WillReturnResult(sqlmock.NewResult(0, 5000))
			affected, err := tc.run(New(db))
			if err != nil || affected != 5000 {
				t.Fatalf("affected = %d, err = %v", affected, err)
			}
		})
	}
}
