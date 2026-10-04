package database

import (
	"regexp"
	"slices"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDropLegacySupportTablesDropsAllThreeChildFirst(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(regexp.QuoteMeta("DROP TABLE IF EXISTS support_sessions, support_entries, support_inboxes")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := dropLegacySupportTables(db); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLegacySupportCleanupIsRegistered(t *testing.T) {
	if !slices.Contains(repairNames(), "support_drop_legacy_tables") {
		t.Fatal("the legacy support cleanup is not registered in runDataRepairs")
	}
	if !slices.Contains(retiredPermissionResources, "support_inboxes") {
		t.Fatal("grants on the removed support_inboxes resource must be retired")
	}
}
