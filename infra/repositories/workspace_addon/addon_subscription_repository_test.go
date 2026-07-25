package workspace_addon_repository

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	workspace_plan "vozko/domain/workspace/workspace_plan"
)

func newMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
		WithoutReturning:     true,
	}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

// TestListReactivatableByWorkspace_FilterAndMapping pins the revival query's filter (active OR
// sweep-expired-but-not-customer-cancelled-and-recent) and that an expired row maps back to the domain.
// The filter is the load-bearing part: it must never revive a customer-cancelled or earlier-cycle addon.
func TestListReactivatableByWorkspace_FilterAndMapping(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := NewAddonSubscriptionRepository(db)

	since := time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)

	// The distinctive fragments of the revival filter must be present: the active branch, and the
	// swept branch gated on cancelled_at IS NULL and current_period_end >=.
	mock.ExpectQuery(`SELECT \* FROM "workspace_addon_subscriptions".*cancelled_at IS NULL.*current_period_end >=`).
		WithArgs(
			"ws-1",
			string(workspace_plan.SubscriptionStatusActive),
			string(workspace_plan.SubscriptionStatusExpired),
			since,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "workspace_id", "addon_definition_id", "addon_key", "entitlement_kind",
			"quantity", "units_per_quantity", "billing_cycle", "status", "unit_price_micros",
			"current_period_start", "current_period_end", "cancelled_at",
		}).AddRow(
			"addon-1", "ws-1", "def-1", "whatsapp_channel", "whatsapp_business_phones",
			1, 1, "monthly", string(workspace_plan.SubscriptionStatusExpired), int64(25_000_000),
			periodEnd.AddDate(0, -1, 0), periodEnd, nil,
		))

	out, err := repo.ListReactivatableByWorkspace("ws-1", since)
	if err != nil {
		t.Fatalf("ListReactivatableByWorkspace: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected one row mapped, got %d", len(out))
	}
	if out[0].ID != "addon-1" || out[0].Status != workspace_plan.SubscriptionStatusExpired {
		t.Fatalf("row mapped wrong: %+v", out[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("query did not match the expected revival filter: %v", err)
	}
}
