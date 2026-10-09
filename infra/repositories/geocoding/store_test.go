package geocoding_repository

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/geocoding"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true, WithoutReturning: true}),
		&gorm.Config{SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

func exact(query string) string {
	quoted := regexp.QuoteMeta(query)
	var b strings.Builder
	n := 0
	for {
		before, after, found := strings.Cut(quoted, `\?`)
		b.WriteString(before)
		if !found {
			break
		}
		n++
		b.WriteString(`\$` + strconv.Itoa(n))
		quoted = after
	}
	return "^" + b.String() + "$"
}

func slot() geocoding.Slot {
	start := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	day := time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)
	ceiling := int64(10)
	return geocoding.Slot{CycleStart: start, NextCycle: start.AddDate(0, 1, 0), Day: day, NextDay: day.AddDate(0, 0, 1), MonthlyLimit: 10, DailyLimit: 3,
		Provider: geocoding.ProviderOpenCage, StoredCeiling: &ceiling}
}

func TestTakeSlotIsOneConditionalUpsert(t *testing.T) {
	for _, want := range []string{"ON CONFLICT (workspace_id) DO UPDATE", "SELECT requests, day_requests FROM taken", "THEN u.requests ELSE 0 END) < ?", "THEN u.day_requests ELSE 0 END) < ?",
		"WHERE EXISTS (SELECT 1 FROM geocoding_settings s WHERE s.workspace_id = ?::uuid AND s.provider = ? AND s.monthly_ceiling IS NOT DISTINCT FROM ?::bigint)"} {
		if !strings.Contains(takeSlotSQL, want) {
			t.Fatalf("take slot SQL misses %q: %s", want, takeSlotSQL)
		}
	}
	if got := strings.Count(takeSlotSQL, "?"); got != 11 {
		t.Fatalf("take slot SQL has %d placeholders, want 11", got)
	}
}

func TestTakeSlotTakesOrReportsTheCurrentUsage(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	s := slot()
	mock.ExpectQuery(exact(takeSlotSQL)).WithArgs("ws-1", s.CycleStart, "ws-1", s.CycleStart, s.Day, sqlmock.AnyArg(), "ws-1", "opencage", int64(10), int64(10), int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "day_requests"}).AddRow(4, 1))
	taken, usage, err := NewUsageStore(db).TakeSlot(context.Background(), "ws-1", s)
	if err != nil || !taken || usage.Requests != 4 || usage.DayRequests != 1 {
		t.Fatalf("TakeSlot() = %v, %+v, %v", taken, usage, err)
	}
	mock.ExpectQuery(exact(takeSlotSQL)).WillReturnRows(sqlmock.NewRows([]string{"requests", "day_requests"}))
	mock.ExpectQuery(exact(readUsageSQL)).WithArgs("ws-1").
		WillReturnRows(sqlmock.NewRows([]string{"cycle_start", "requests", "day", "day_requests"}).AddRow(s.CycleStart, 10, s.Day, 2))
	taken, usage, err = NewUsageStore(db).TakeSlot(context.Background(), "ws-1", s)
	if err != nil || taken || usage.Requests != 10 {
		t.Fatalf("a refused slot = %v, %+v, %v, want the usage that refused it", taken, usage, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTakeSlotWithoutRoomNeverWrites(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	empty := slot()
	empty.DailyLimit = 0
	if taken, _, err := NewUsageStore(db).TakeSlot(context.Background(), "ws-1", empty); taken || err != nil {
		t.Fatalf("TakeSlot() with no room = %v, %v", taken, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsReadMapsTheRow(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(exact(readSettingsSQL)).WithArgs("ws-1").
		WillReturnRows(sqlmock.NewRows([]string{"workspace_id", "provider", "provider_changed_by", "provider_changed_at", "monthly_ceiling", "ceiling_changed_by", "ceiling_changed_at"}).
			AddRow("ws-1", "opencage", "owner-1", at, nil, nil, nil))
	got, err := NewSettingsStore(db).Settings(context.Background(), "ws-1")
	if err != nil || got.Provider != geocoding.ProviderOpenCage || got.ProviderChangedBy != "owner-1" || got.MonthlyCeiling != nil || got.Ceiling() != geocoding.DefaultMonthlyCeiling {
		t.Fatalf("Settings() = %+v, %v", got, err)
	}
}

func TestSettingsOfAnUnknownWorkspaceAreOff(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(exact(readSettingsSQL)).WithArgs("ws-1").WillReturnRows(sqlmock.NewRows([]string{"workspace_id"}))
	got, err := NewSettingsStore(db).Settings(context.Background(), "ws-1")
	if err != nil || got.Enabled() || got.Ceiling() != 0 {
		t.Fatalf("Settings() = %+v, %v, want no provider and no ceiling", got, err)
	}
}

func TestTakeSlotWithoutTheSettingsItCameFromNeverWrites(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	orphan := slot()
	orphan.Provider = ""
	if taken, _, err := NewUsageStore(db).TakeSlot(context.Background(), "ws-1", orphan); taken || err != nil {
		t.Fatalf("TakeSlot() without a provider = %v, %v", taken, err)
	}
	unset := slot()
	unset.StoredCeiling = nil
	mock.ExpectQuery(exact(takeSlotSQL)).WithArgs("ws-1", unset.CycleStart, "ws-1", unset.CycleStart, unset.Day, sqlmock.AnyArg(), "ws-1", "opencage", nil, int64(10), int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "day_requests"}).AddRow(1, 1))
	if taken, _, err := NewUsageStore(db).TakeSlot(context.Background(), "ws-1", unset); !taken || err != nil {
		t.Fatalf("TakeSlot() under the default ceiling = %v, %v", taken, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
