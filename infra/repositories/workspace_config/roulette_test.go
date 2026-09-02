package workspace_config_repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	wsc "vozko/domain/workspace_config"
)

func newConfigDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
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

// The rescue sweep drives off this list, so the WHERE clause is what makes the
// sweep free for every workspace on the default mode — and what makes flipping
// the mode back stop pending rescues with no cleanup pass.
func TestListRoulettePolicies_FiltersToLastSeenWithRescueOn(t *testing.T) {
	db, mock, sqlDB := newConfigDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT workspace_id, roulette_last_seen_window_hours, roulette_rescue_after_minutes, working_hours ` +
		`FROM "workspace_configs" ` +
		`WHERE roulette_mode = \$1 AND roulette_rescue_enabled = \$2`).
		WithArgs(wsc.RouletteModeLastSeen, true).
		WillReturnRows(sqlmock.NewRows(
			[]string{"workspace_id", "roulette_last_seen_window_hours", "roulette_rescue_after_minutes", "working_hours"}))

	if _, err := NewRepository(db).ListRoulettePolicies(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A row written before these columns existed, or edited by hand, must not be
// able to hand the sweep a zero deadline that rescues every conversation
// instantly.
func TestListRoulettePolicies_ClampsOutOfRangeRows(t *testing.T) {
	db, mock, sqlDB := newConfigDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT workspace_id`).WillReturnRows(
		sqlmock.NewRows([]string{"workspace_id", "roulette_last_seen_window_hours", "roulette_rescue_after_minutes"}).
			AddRow("ws-zero", 0, 0).
			AddRow("ws-huge", 99999, 99999).
			AddRow("ws-sane", 24, 30),
	)

	got, err := NewRepository(db).ListRoulettePolicies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d policies", len(got))
	}

	byID := map[string]wsc.RoulettePolicy{}
	for _, p := range got {
		byID[p.WorkspaceID] = p
	}
	if byID["ws-zero"].RescueAfter != 15*time.Minute ||
		byID["ws-zero"].LastSeenWindow != 48*time.Hour {
		t.Fatalf("zero row must fall back to the defaults, got %+v", byID["ws-zero"])
	}
	if byID["ws-huge"].RescueAfter != 1440*time.Minute ||
		byID["ws-huge"].LastSeenWindow != 168*time.Hour {
		t.Fatalf("out-of-range row must clamp to the ceilings, got %+v", byID["ws-huge"])
	}
	if byID["ws-sane"].RescueAfter != 30*time.Minute ||
		byID["ws-sane"].LastSeenWindow != 24*time.Hour {
		t.Fatalf("an in-range row must be preserved, got %+v", byID["ws-sane"])
	}
}

// A workspace with no config row gets the defaults, which are the historical
// behaviour. This is the guard that keeps the feature dark on upgrade.
func TestGetByWorkspaceID_MissingRowDefaultsToTheOnlineMode(t *testing.T) {
	db, mock, sqlDB := newConfigDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "workspace_configs"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	cfg, err := NewRepository(db).GetByWorkspaceID(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EffectiveRouletteMode() != wsc.RouletteModeOnline {
		t.Fatalf("got mode %q", cfg.EffectiveRouletteMode())
	}
	if cfg.RouletteRescueActive() {
		t.Fatal("a workspace with no config row must not have rescue active")
	}
}

// Normalized on read, the same way the auto-close hours already are: an unknown
// mode from hand-written SQL must resolve to the historical behaviour rather
// than putting the roulette into a mode that does not exist.
func TestGetByWorkspaceID_NormalizesOnRead(t *testing.T) {
	db, mock, sqlDB := newConfigDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "workspace_configs"`).WillReturnRows(
		sqlmock.NewRows([]string{
			"id", "workspace_id", "roulette_mode",
			"roulette_last_seen_window_hours", "roulette_rescue_after_minutes",
			"roulette_rescue_enabled",
		}).AddRow("cfg-1", "ws-1", "garbage", 0, 99999, true),
	)

	cfg, err := NewRepository(db).GetByWorkspaceID(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RouletteMode != wsc.RouletteModeOnline {
		t.Fatalf("got mode %q, want it normalized to the default", cfg.RouletteMode)
	}
	if cfg.RouletteLastSeenWindowHours != wsc.DefaultRouletteLastSeenWindowHours {
		t.Fatalf("got window %d", cfg.RouletteLastSeenWindowHours)
	}
	if cfg.RouletteRescueAfterMinutes != wsc.MaxRouletteRescueAfterMinutes {
		t.Fatalf("got rescue %d", cfg.RouletteRescueAfterMinutes)
	}
}
