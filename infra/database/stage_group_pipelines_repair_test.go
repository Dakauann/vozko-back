package database

import (
	"database/sql"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The backfill is the only part of the funnel work that reaches the database with
// hand-written SQL, and it runs at boot on every deployment. These pin the queries
// and, more importantly, the ORDER and the no-op cases: a repair that is not a
// no-op on a healthy database is a repair that corrupts one.

func newRepairDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, *sql.DB) {
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

func TestMaterializeStageGroupPipelines_NoOrphansWritesNothing(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM stage_groups g`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name"}))

	if err := materializeStageGroupPipelines(db); err != nil {
		t.Fatalf("a healthy database must repair cleanly: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMaterializeStageGroupPipelines_CreatesTheFunnelAndItsStages(t *testing.T) {
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM stage_groups g`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name"}).
			AddRow("grp-1", "ws-1", "Pós-venda"))

	mock.ExpectQuery(`FROM stage_group_items`).
		WillReturnRows(sqlmock.NewRows([]string{"name", "description", "color", "position"}).
			AddRow("  Triagem  ", "primeiro contato", "#3b82f6", 1).
			AddRow("Resolvido", "encerrado", "#10b981", 2))

	mock.ExpectQuery(`COALESCE\(MAX\(position\), 0\) FROM pipelines`).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(3))

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO pipelines`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO stages`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO stages`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := materializeStageGroupPipelines(db); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	// The ordering matters: read the orphans, read their items, find the next
	// position, then insert. sqlmock enforces it in order by default.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMaterializeStageGroupPipelines_SkipsAnEmptyGroup(t *testing.T) {
	// An empty group would produce a funnel with no columns — a board nobody can
	// add to. Leaving it alone is the correct outcome, not creating one.
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM stage_groups g`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "workspace_id", "name"}).
			AddRow("grp-empty", "ws-1", "Vazio"))
	mock.ExpectQuery(`FROM stage_group_items`).
		WillReturnRows(sqlmock.NewRows([]string{"name", "description", "color", "position"}))

	if err := materializeStageGroupPipelines(db); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	// No INSERT was expected; sqlmock fails the test if one is issued.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestMaterializeStageGroupPipelines_MissingTablesDoNotAbortBoot(t *testing.T) {
	// A deployment that never used stage groups has nothing to repair. This runs
	// inside the migration transaction, so returning an error here would stop the
	// server from starting.
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`FROM stage_groups g`).
		WillReturnError(sql.ErrConnDone)

	if err := materializeStageGroupPipelines(db); err != nil {
		t.Fatalf("a missing table must be skipped, not fatal: %v", err)
	}
}

func TestMaterializeStageGroupPipelines_IsRegisteredInTheRepairList(t *testing.T) {
	// Easy to write and never wire up; the repair only runs because it is listed.
	db, mock, sqlDB := newRepairDB(t)
	defer sqlDB.Close()
	_ = db

	found := false
	for _, name := range repairNames() {
		if name == "stg_materialize_stage_group_pipelines" {
			found = true
		}
	}
	if !found {
		t.Fatal("the repair is not registered in runDataRepairs")
	}
	_ = mock
}
