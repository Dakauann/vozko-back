package lead_memory_repository

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"vozko/domain/actor"
	leadmemory "vozko/domain/lead_memory"
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
	}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock, sqlDB
}

var createdAt = time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

func memoryRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "workspace_id", "lead_id", "category", "content", "actor_kind", "actor_id", "created_at", "updated_at"}).
		AddRow("11111111-2222-3333-4444-555555555555", "ws-1", "lead-1", "preference", "Prefere boleto.", "ai", "ai:agent-1", createdAt, createdAt)
}

func TestCreateTranslatesDuplicateKey(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`INSERT INTO "lead_memories"`).
		WillReturnError(errors.New(`ERROR: duplicate key value violates unique constraint "ux_lead_memories_dedup" (SQLSTATE 23505)`))

	err := NewRepository(db).Create(&leadmemory.LeadMemory{
		WorkspaceID: "ws-1", LeadID: "lead-1",
		Category: leadmemory.CategoryPreference, Content: "Prefere boleto.",
		ActorKind: actor.KindAI, ActorID: "ai:agent-1",
	})
	if !errors.Is(err, leadmemory.ErrDuplicate) {
		t.Fatalf("Create on unique violation = %v, want ErrDuplicate", err)
	}
}

func TestFindByIDPrefixResolution(t *testing.T) {
	t.Run("a short prefix never reaches the database", func(t *testing.T) {
		db, _, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		_, err := NewRepository(db).FindByIDPrefix("ws-1", "lead-1", "1111")
		if !errors.Is(err, leadmemory.ErrNotFound) {
			t.Fatalf("short prefix = %v, want ErrNotFound", err)
		}
	})

	t.Run("one match resolves", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		// The query must be lead-scoped and workspace-scoped: a prefix is only
		// meaningful within one lead's prompt block.
		mock.ExpectQuery(`SELECT .* FROM "lead_memories" WHERE \(workspace_id = .* AND lead_id = .* AND id::text LIKE .*\)`).
			WithArgs("ws-1", "lead-1", "11111111%", 2).
			WillReturnRows(memoryRow())
		got, err := NewRepository(db).FindByIDPrefix("ws-1", "lead-1", "11111111")
		if err != nil || got == nil || got.ID != "11111111-2222-3333-4444-555555555555" {
			t.Fatalf("resolve = (%+v, %v)", got, err)
		}
		if got.ActorKind != actor.KindAI {
			t.Fatalf("actor kind lost in mapping: %q", got.ActorKind)
		}
	})

	t.Run("two matches are ambiguous", func(t *testing.T) {
		db, mock, sqlDB := newMockDB(t)
		defer sqlDB.Close()
		rows := memoryRow().
			AddRow("11111111-9999-3333-4444-555555555555", "ws-1", "lead-1", "deal", "Outro fato.", "human", "user-1", createdAt, createdAt)
		mock.ExpectQuery(`SELECT .* FROM "lead_memories"`).WillReturnRows(rows)
		_, err := NewRepository(db).FindByIDPrefix("ws-1", "lead-1", "11111111")
		if !errors.Is(err, leadmemory.ErrAmbiguousID) {
			t.Fatalf("two matches = %v, want ErrAmbiguousID", err)
		}
	})
}

func TestListByLeadFiltersAndPages(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectQuery(`SELECT count\(\*\) FROM "lead_memories" WHERE \(workspace_id = .* AND lead_id = .*\) AND category = .*`).
		WithArgs("ws-1", "lead-1", "preference").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	// Newest first: the prompt block and the panel both lead with what was
	// learned last.
	mock.ExpectQuery(`SELECT .* FROM "lead_memories" .* ORDER BY created_at DESC, id DESC`).
		WillReturnRows(memoryRow())

	cat := leadmemory.CategoryPreference
	items, total, err := NewRepository(db).ListByLead("ws-1", "lead-1", leadmemory.ListQuery{Category: &cat})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("ListByLead = (%d items, total %d, %v)", len(items), total, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestUpdateGuardsWorkspaceAndReportsMissing(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	mock.ExpectExec(`UPDATE "lead_memories" SET .* WHERE \(workspace_id = .* AND id = .*\)`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := NewRepository(db).Update(&leadmemory.LeadMemory{
		ID: "m-1", WorkspaceID: "ws-other",
		Category: leadmemory.CategoryDeal, Content: "x",
		ActorKind: actor.KindHuman, ActorID: "user-1",
	})
	if !errors.Is(err, leadmemory.ErrNotFound) {
		t.Fatalf("update of foreign/missing row = %v, want ErrNotFound", err)
	}
}

func TestSoftDeleteIsGuardedUpdate(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()

	// Soft delete must be an UPDATE (deleted_at), never a DELETE: forgotten
	// memories stay for audit and only leave via the lead's own lifecycle.
	mock.ExpectExec(`UPDATE "lead_memories" SET "deleted_at"=.* WHERE \(workspace_id = .* AND id = .*\)`).
		WithArgs(sqlmock.AnyArg(), "ws-1", "m-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := NewRepository(db).SoftDelete("ws-1", "m-1"); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
