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

// The crash-safety property, and the single most load-bearing line in the
// last_seen roulette.
//
// An interval whose owner's replica died is left with ended_at NULL forever.
// Reading it as NOW() would make that departed agent permanently the freshest
// candidate in the ring — they would receive every conversation and never look
// at one. COALESCE(ended_at, started_at) reports the last moment we can prove
// they were present; a genuinely-online agent is covered by the resolver's
// live connected-set overlay instead.
func TestLastSeen_UsesStartedAtForOpenIntervalsNotNow(t *testing.T) {
	db, mock, sqlDB := newPresenceDB(t)
	defer sqlDB.Close()

	// sqlmock is in regexp-matcher mode, so this is an assertion on the SQL the
	// repository actually emits: a COALESCE(ended_at, NOW()) would not match and
	// the expectation would fail.
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

// An empty ask must not reach the database at all: the roulette calls this on
// every inbound message, and a workspace with an empty roster would otherwise
// pay a query to be told nothing.
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
	// No ExpectQuery was registered, so any query would fail the expectations.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Users with no presence history must be ABSENT from the map, not zero-valued:
// the ring distinguishes "never online" (dropped) from "online at some point"
// (ordered), and a zero time would look like an ancient but real timestamp.
func TestLastSeen_UsersWithoutHistoryAreAbsent(t *testing.T) {
	db, mock, sqlDB := newPresenceDB(t)
	defer sqlDB.Close()

	seen := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT`).WillReturnRows(
		sqlmock.NewRows([]string{"user_id", "last_seen"}).
			AddRow("u-1", seen).
			AddRow("", seen).           // defensive: no user id
			AddRow("u-3", time.Time{}), // defensive: zero timestamp
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
