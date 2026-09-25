package database

import (
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestConcurrentIndexBuildsWhenNothingIsThere(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WithArgs("idx_x").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectExec(`CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := createIndexConcurrently(db, concurrentIndex{name: "idx_x", sql: "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x ON t (a)"}); err != nil {
		t.Fatalf("createIndexConcurrently() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestConcurrentIndexReplacesAnInvalidLeftover(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WithArgs("idx_x").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectExec(`DROP INDEX CONCURRENTLY IF EXISTS idx_x`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := createIndexConcurrently(db, concurrentIndex{name: "idx_x", sql: "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x ON t (a)"}); err != nil {
		t.Fatalf("createIndexConcurrently() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestConcurrentIndexReportsAFailedValidityCheck(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`NOT i\.indisvalid`).WillReturnError(errors.New("catalog unavailable"))

	if err := createIndexConcurrently(db, concurrentIndex{name: "idx_x", sql: "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_x ON t (a)"}); err == nil {
		t.Fatalf("createIndexConcurrently() built an index without knowing whether an invalid one was in the way")
	}
}

func TestEveryConcurrentIndexIsBuiltConcurrently(t *testing.T) {
	for _, idx := range concurrentIndexes() {
		if !strings.Contains(strings.Join(strings.Fields(idx.sql), " "), "CREATE INDEX CONCURRENTLY IF NOT EXISTS "+idx.name+" ON ") {
			t.Fatalf("%s is not built concurrently under its own name: %s", idx.name, idx.sql)
		}
	}
}

func TestAnalyticsIndexesCoverTheirQueries(t *testing.T) {
	want := map[string]string{
		"idx_conv_event_ws_type_created": "conversation_events (workspace_id, event_type, created_at)",
		"idx_ca_waiting":                 "audience_analyses (workspace_id) WHERE status IN ('pending', 'in_flight') AND deleted_at IS NULL",
	}
	found := map[string]string{}
	for _, idx := range concurrentIndexes() {
		found[idx.name] = strings.Join(strings.Fields(idx.sql), " ")
	}
	for name, shape := range want {
		sql, ok := found[name]
		if !ok {
			t.Fatalf("%s is missing", name)
		}
		if !strings.Contains(sql, shape) {
			t.Fatalf("%s = %q, want it to contain %q", name, sql, shape)
		}
	}
}

func TestTheSenderBackfillFindsItsRowsThroughAPartialIndex(t *testing.T) {
	for _, idx := range concurrentIndexes() {
		if idx.name == "idx_cm_sender_unknown" {
			if !strings.Contains(idx.sql, "WHERE sender_kind = 'unknown'") {
				t.Fatalf("the index must cover only unattributed rows: %s", idx.sql)
			}
			return
		}
	}
	t.Fatal("idx_cm_sender_unknown is not built")
}

func TestUnreadCountsHaveAnIndexOnTheContactsMessages(t *testing.T) {
	for _, idx := range concurrentIndexes() {
		if idx.name == "idx_cm_unread_contact" {
			if !strings.Contains(idx.sql, "WHERE read = false AND deleted_at IS NULL AND "+SentByContactSQL("")) {
				t.Fatalf("the index predicate must match the unread queries: %s", idx.sql)
			}
			return
		}
	}
	t.Fatal("idx_cm_unread_contact is not built")
}
