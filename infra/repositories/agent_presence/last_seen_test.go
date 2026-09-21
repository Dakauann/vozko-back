package agent_presence_repository

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newPresenceDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	mock.MatchExpectationsInOrder(false)
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
	return db, mock, sqlDB
}

func TestLastSeen_UsesStartedAtForOpenIntervalsNotNow(t *testing.T) {
	db, mock, sqlDB := newPresenceDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT user_id, MAX\(COALESCE\(ended_at, started_at\)\) AS last_seen.*` +
		`FROM "agent_presence_intervals".*` +
		`WHERE workspace_id = .* AND user_id IN .* AND state IN .*` +
		`GROUP BY "user_id"`).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "last_seen"}))

	if _, err := New(db).LastSeen("ws-1", []string{"u-1"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLastSeen_EmptyInputSkipsTheQuery(t *testing.T) {
	db, mock, sqlDB := newPresenceDB(t)
	defer sqlDB.Close()

	got, err := New(db).LastSeen("ws-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if _, err := New(db).LastSeen("", []string{"u-1"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLastSeen_UsersWithoutHistoryAreAbsent(t *testing.T) {
	db, mock, sqlDB := newPresenceDB(t)
	defer sqlDB.Close()

	seen := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT`).WillReturnRows(
		sqlmock.NewRows([]string{"user_id", "last_seen"}).
			AddRow("u-1", seen).
			AddRow("", seen).
			AddRow("u-3", time.Time{}),
	)

	got, err := New(db).LastSeen("ws-1", []string{"u-1", "u-2", "u-3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %v, want only u-1", got)
	}
	if !got["u-1"].Equal(seen) {
		t.Fatalf("got %v want %v", got["u-1"], seen)
	}
	if _, ok := got["u-2"]; ok {
		t.Fatal("a user with no presence history must be absent from the map")
	}
}
