package workspace_config_repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	ca "vozko/domain/audience"
)

// The audience engine's two settings share a row with the roulette policy, the
// auto-close windows and the working hours, and two different domains write
// that row. These cases pin the two properties that keeps safe.

// One: a targeted write. Anything but these two columns must be left alone, or
// setting an analysis budget would quietly reset how conversations are
// distributed.
func TestAudienceSettingsSaveTouchesOnlyItsOwnColumns(t *testing.T) {
	db, mock, sqlDB := newConfigDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT count\(\*\) FROM "workspace_configs"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec(`UPDATE "workspace_configs" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	store := NewAudienceSettingsStore(db)
	err := store.Save(context.Background(), "ws-1", ca.WorkspaceSettings{DailyCap: 500, DebounceMinutes: 30})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Two: the OTHER domain's read-modify-write has to carry these columns through.
// GetByWorkspaceID feeds Upsert on every workspace-config change, so a field
// missing from either mapping is a field that gets zeroed the next time an
// admin touches the roulette.
func TestWorkspaceConfigRoundTripsTheAudienceColumns(t *testing.T) {
	db, mock, sqlDB := newConfigDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT \* FROM "workspace_configs"`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "workspace_id", "audience_daily_cap", "audience_debounce_minutes",
		}).AddRow("cfg-1", "ws-1", 500, 30))

	repo := NewRepository(db)
	cfg, err := repo.GetByWorkspaceID(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AudienceDailyCap != 500 {
		t.Errorf("AudienceDailyCap = %d, want 500 read back", cfg.AudienceDailyCap)
	}
	if cfg.AudienceDebounceMinutes != 30 {
		t.Errorf("AudienceDebounceMinutes = %d, want 30 read back", cfg.AudienceDebounceMinutes)
	}

	// And back out again, unchanged, the way an unrelated config update would.
	mock.ExpectExec(`(UPDATE|INSERT INTO) "workspace_configs"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	if err := repo.Upsert(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// A workspace that never configured anything reads as zeroes, which each caller
// resolves against its own default. Not an error: it is most workspaces.
func TestAudienceSettingsGetTreatsAMissingRowAsUnset(t *testing.T) {
	db, mock, sqlDB := newConfigDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT .* FROM "workspace_configs"`).
		WillReturnRows(sqlmock.NewRows([]string{"audience_daily_cap", "audience_debounce_minutes"}))

	got, err := NewAudienceSettingsStore(db).Get(context.Background(), "ws-none")
	if err != nil {
		t.Fatalf("a missing row was reported as an error: %v", err)
	}
	if got != (ca.WorkspaceSettings{}) {
		t.Errorf("Get = %+v, want zeroes", got)
	}
	if ca.ClampDebounceMinutes(got.DebounceMinutes) != ca.DefaultDebounceMinutes {
		t.Error("an unset debounce did not resolve to the historical default")
	}
}

// Only the workspaces that CHANGED the window, so a deployment where nobody did
// gets an empty map and the sweep never has to attribute an entry at all.
func TestConfiguredDebounceWindowsSkipsUnsetWorkspaces(t *testing.T) {
	db, mock, sqlDB := newConfigDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT .* FROM "workspace_configs" WHERE audience_debounce_minutes > 0`).
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "audience_debounce_minutes"}).
			AddRow("ws-slow", 30).
			// Out of range in the database is clamped on the way out, never
			// allowed to stall the sweep.
			AddRow("ws-corrupt", ca.MaxDebounceMinutes+999))

	got, err := NewAudienceSettingsStore(db).ConfiguredDebounceWindows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["ws-slow"] != ca.DebounceWindow(30) {
		t.Errorf("ws-slow window = %v, want 30m", got["ws-slow"])
	}
	if got["ws-corrupt"] != ca.DebounceWindow(ca.MaxDebounceMinutes) {
		t.Errorf("ws-corrupt window = %v, want it clamped to the maximum", got["ws-corrupt"])
	}
	if _, present := got["ws-default"]; present {
		t.Error("a workspace that never set a window appeared in the map")
	}
}
