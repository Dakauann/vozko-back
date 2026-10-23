package googlesheet_repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"vozko/domain/googlesheet"
	"vozko/domain/shared"
	"vozko/infra/crypto/pii"
	"vozko/infra/crypto/piigorm"
	"vozko/infra/crypto/vault"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/testenv"
)

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testenv.Postgres(t).DSN()
	v, err := vault.New(bytes.Repeat([]byte{0x11}, 32), 1)
	if err != nil {
		t.Fatal(err)
	}
	service, err := pii.New(map[byte]*vault.Vault{1: v}, 1, bytes.Repeat([]byte{0x22}, 32))
	if err != nil {
		t.Fatal(err)
	}
	piigorm.SetService(service)
	t.Cleanup(func() { piigorm.SetService(nil) })
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "gsheet_test_" + uuid.New().String()[:8]
	if err := admin.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schemaName), silent)
	if err != nil {
		t.Fatalf("open schema: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(schema.GoogleSheetModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range []string{
		"ux_google_sheet_tab", "ux_google_sheet_rows_key", "idx_google_sheet_ws_created", "idx_google_sheet_rows_claim",
		"idx_google_sheet_rows_ready", "idx_google_sheet_rows_in_flight", "idx_google_sheet_rows_problems",
		"idx_google_sheet_rows_written", "idx_google_sheet_rows_purge", "idx_google_sheet_rows_lead",
	} {
		stmt, ok := database.PerformanceIndexSQL(name)
		if !ok {
			t.Fatalf("index %s is not declared", name)
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("index %s: %v", name, err)
		}
	}
	return db
}

func storedSheet(t *testing.T, repo googlesheet.SheetRepository) *googlesheet.Sheet {
	t.Helper()
	s, err := googlesheet.NewSheet(googlesheet.NewSheetInput{
		WorkspaceID: uuid.NewString(), AccountID: uuid.NewString(), Title: "Pedidos", TabName: "Pedidos",
		Columns: []googlesheet.ColumnInput{{Header: "Nome", Type: googlesheet.TypeText, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.SpreadsheetID, s.TabID = "spreadsheet-"+uuid.NewString()[:6], 0
	if err := repo.Create(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	return s
}

func pendingRow(t *testing.T, s *googlesheet.Sheet, key string, at time.Time) *googlesheet.Row {
	t.Helper()
	row, err := googlesheet.NewRow(s, googlesheet.Cells{"nome": "Ana " + key}, key, googlesheet.Origin{Kind: googlesheet.OriginWorkflow, RunID: "run-1", LeadID: "lead-1"}, at)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSheetRepositoryRoundTripAndOptimisticVersion(t *testing.T) {
	db := integrationDB(t)
	repo := NewSheetRepository(db)
	ctx := context.Background()
	s := storedSheet(t, repo)

	got, err := repo.FindInWorkspace(ctx, s.WorkspaceID, s.ID)
	if err != nil || got.Columns[0].Key != "nome" || got.Version != 1 {
		t.Fatalf("FindInWorkspace = %+v, %v", got, err)
	}
	if _, err := repo.FindInWorkspace(ctx, uuid.NewString(), s.ID); !errors.Is(err, googlesheet.ErrSheetNotFound) {
		t.Fatalf("another workspace read the sheet: %v", err)
	}
	if err := got.EditColumns(googlesheet.ColumnsEdit{Add: []googlesheet.ColumnInput{{Header: "Valor", Type: googlesheet.TypeMoney}}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(ctx, got, 1); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := repo.Update(ctx, got, 1); !errors.Is(err, googlesheet.ErrStaleSheet) {
		t.Fatalf("a stale version was written: %v", err)
	}
	list, err := repo.List(ctx, googlesheet.ListInput{WorkspaceID: s.WorkspaceID, Search: "ped", Pagination: shared.Pagination{Page: 1, PageSize: 10}})
	if err != nil || len(list.Items) != 1 || len(list.Items[0].Columns) != 2 {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if err := repo.Delete(ctx, s.WorkspaceID, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, s.ID); !errors.Is(err, googlesheet.ErrSheetNotFound) {
		t.Fatalf("deleted sheet still readable: %v", err)
	}
}

func TestInsertIsIdempotentPerSheetAndKey(t *testing.T) {
	db := integrationDB(t)
	sheets, rows := NewSheetRepository(db), NewRowRepository(db)
	ctx := context.Background()
	s := storedSheet(t, sheets)
	now := time.Now().UTC().Truncate(time.Millisecond)

	first, duplicate, err := rows.Insert(ctx, pendingRow(t, s, "k1", now))
	if err != nil || duplicate {
		t.Fatalf("first insert = %v, duplicate %v", err, duplicate)
	}
	again, duplicate, err := rows.Insert(ctx, pendingRow(t, s, "k1", now))
	if err != nil || !duplicate || again.ID != first.ID {
		t.Fatalf("retry made a second row: %v, duplicate %v, ids %s vs %s", err, duplicate, again.ID, first.ID)
	}
	var count int64
	db.Model(&schema.GoogleSheetRow{}).Count(&count)
	if count != 1 {
		t.Fatalf("rows = %d, want 1", count)
	}
	var raw []byte
	if err := db.Raw("SELECT cells FROM google_sheet_rows WHERE id = ?", first.ID).Row().Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("Ana")) {
		t.Fatal("row values are stored in plaintext")
	}
}

func TestClaimTakesTheOldestRowsAndWaitsWhileTheHeadBacksOff(t *testing.T) {
	db := integrationDB(t)
	sheets, rows := NewSheetRepository(db), NewRowRepository(db)
	ctx := context.Background()
	s := storedSheet(t, sheets)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Millisecond)
	for i := 0; i < 5; i++ {
		if _, _, err := rows.Insert(ctx, pendingRow(t, s, fmt.Sprintf("k%d", i), base.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	claimed, err := rows.Claim(ctx, s.ID, uuid.NewString(), 3, now)
	if err != nil || len(claimed) != 3 {
		t.Fatalf("Claim = %d rows, %v", len(claimed), err)
	}
	for i, row := range claimed {
		if row.Cells["nome"] != fmt.Sprintf("Ana k%d", i) || row.Status != googlesheet.RowInFlight || row.Attempts != 1 {
			t.Fatalf("claimed[%d] = %+v", i, row)
		}
	}
	if err := rows.Unclaim(ctx, []string{claimed[2].ID}); err != nil {
		t.Fatal(err)
	}
	var attempts int
	db.Raw("SELECT attempts FROM google_sheet_rows WHERE id = ?", claimed[2].ID).Scan(&attempts)
	if attempts != 0 {
		t.Fatalf("an unclaimed row kept the attempt it never made: %d", attempts)
	}
	again, err := rows.Claim(ctx, s.ID, uuid.NewString(), 1, now)
	if err != nil || len(again) != 1 || again[0].ID != claimed[2].ID {
		t.Fatalf("the unclaimed row is not next in line: %+v, %v", again, err)
	}
	ids := []string{claimed[0].ID, claimed[1].ID, claimed[2].ID}
	if err := rows.Release(ctx, ids, now.Add(time.Hour), "rate_limited"); err != nil {
		t.Fatal(err)
	}
	blocked, err := rows.Claim(ctx, s.ID, uuid.NewString(), 10, now)
	if err != nil || len(blocked) != 0 {
		t.Fatalf("rows behind a backing off head were claimed out of order: %d, %v", len(blocked), err)
	}
	pending, err := rows.PendingSheets(ctx, now, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("PendingSheets listed a backing off sheet: %+v, %v", pending, err)
	}
	pending, err = rows.PendingSheets(ctx, now.Add(2*time.Hour), 10)
	if err != nil || len(pending) != 1 || pending[0].SheetID != s.ID {
		t.Fatalf("PendingSheets = %+v, %v", pending, err)
	}
}

func TestConcurrentClaimsNeverShareARow(t *testing.T) {
	db := integrationDB(t)
	sheets, rows := NewSheetRepository(db), NewRowRepository(db)
	ctx := context.Background()
	s := storedSheet(t, sheets)
	base := time.Now().UTC().Add(-time.Minute)
	for i := 0; i < 60; i++ {
		if _, _, err := rows.Insert(ctx, pendingRow(t, s, fmt.Sprintf("k%02d", i), base.Add(time.Duration(i)*time.Millisecond))); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				claimed, err := rows.Claim(ctx, s.ID, uuid.NewString(), 7, time.Now().UTC())
				if err != nil {
					t.Error(err)
					return
				}
				if len(claimed) == 0 {
					return
				}
				mu.Lock()
				for _, row := range claimed {
					if seen[row.ID] {
						t.Errorf("row %s claimed twice", row.ID)
					}
					seen[row.ID] = true
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != 60 {
		t.Fatalf("claimed %d of 60 rows", len(seen))
	}
}

func TestMarkWrittenHealthRequeueAndPurge(t *testing.T) {
	db := integrationDB(t)
	sheets, rows := NewSheetRepository(db), NewRowRepository(db)
	ctx := context.Background()
	s := storedSheet(t, sheets)
	base := time.Now().UTC().Add(-time.Minute)
	for i := 0; i < 4; i++ {
		if _, _, err := rows.Insert(ctx, pendingRow(t, s, fmt.Sprintf("k%d", i), base.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	claimed, err := rows.Claim(ctx, s.ID, uuid.NewString(), 4, time.Now().UTC())
	if err != nil || len(claimed) != 4 {
		t.Fatal(err)
	}
	writtenAt := time.Now().UTC().Add(-40 * 24 * time.Hour)
	if err := rows.MarkWritten(ctx, []googlesheet.WrittenRow{{ID: claimed[0].ID, RowNumber: 2}, {ID: claimed[1].ID, RowNumber: 3}}, writtenAt); err != nil {
		t.Fatal(err)
	}
	if err := rows.Hold(ctx, []string{claimed[2].ID}, "google_sheet_access_lost", "403"); err != nil {
		t.Fatal(err)
	}
	if err := rows.Fail(ctx, claimed[3].ID, "google_sheet_invalid_value", "bad"); err != nil {
		t.Fatal(err)
	}
	stuck, err := rows.StuckInFlight(ctx, s.ID, time.Now().UTC().Add(time.Hour))
	if err != nil || len(stuck) != 0 {
		t.Fatalf("finished rows reported stuck: %d, %v", len(stuck), err)
	}
	health, err := rows.Health(ctx, s.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if health.Written != 2 || health.Held != 1 || health.Failed != 1 || health.Pending != 0 || len(health.Problems) != 2 {
		t.Fatalf("health = %+v", health)
	}
	requeued, err := rows.RequeueProblems(ctx, s.ID, time.Now().UTC())
	if err != nil || requeued != 2 {
		t.Fatalf("RequeueProblems = %d, %v", requeued, err)
	}
	purged, err := rows.PurgeWritten(ctx, time.Now().UTC().Add(-30*24*time.Hour), 100)
	if err != nil || purged != 2 {
		t.Fatalf("PurgeWritten = %d, %v", purged, err)
	}
	removed, err := rows.DeleteForLead(ctx, s.WorkspaceID, "lead-1")
	if err != nil || removed != 2 {
		t.Fatalf("DeleteForLead = %d, %v", removed, err)
	}
}
