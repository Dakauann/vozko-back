package lead

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func dialDirectoryDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("VOZKO_TEST_DB") != "1" {
		t.Skip("set VOZKO_TEST_DB=1 (and DB_* vars) to run against Postgres")
	}
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_PORT"))
	silent := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), silent)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	schemaName := "lead_dial_test_" + uuid.New().String()[:8]
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
	for _, ddl := range []string{
		`CREATE TABLE leads (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, number varchar(20), name varchar(255), blocked boolean NOT NULL DEFAULT false,
			opted_out_at timestamptz, version bigint NOT NULL DEFAULT 1, created_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`,
		`CREATE TABLE lead_phones (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, lead_id uuid NOT NULL, number varchar(20) NOT NULL,
			label varchar(16) NOT NULL, position int NOT NULL DEFAULT 0, created_at timestamptz NOT NULL DEFAULT now())`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	return db
}

func TestIntegrationFindIdentitiesMatchesBothNinthDigitFormsAndSkipsContactPhones(t *testing.T) {
	db := dialDirectoryDB(t)
	ws, otherWS := uuid.NewString(), uuid.NewString()
	owner, relative, deleted, stranger := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	seed := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO leads (id, workspace_id, number, name, blocked) VALUES (?, ?, '558494409684', 'Maria', true)`, []any{owner, ws}},
		{`INSERT INTO leads (id, workspace_id, number, name) VALUES (?, ?, '5511987654321', 'Filho')`, []any{relative, ws}},
		{`INSERT INTO lead_phones (id, workspace_id, lead_id, number, label) VALUES (?, ?, ?, '5584994409684', 'mobile')`, []any{uuid.NewString(), ws, relative}},
		{`INSERT INTO leads (id, workspace_id, number, name, deleted_at) VALUES (?, ?, '5584994409684', 'Apagada', now())`, []any{deleted, ws}},
		{`INSERT INTO leads (id, workspace_id, number, name) VALUES (?, ?, '5584994409684', 'Outro workspace')`, []any{stranger, otherWS}},
	}
	for _, row := range seed {
		if err := db.Exec(row.sql, row.args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	directory := NewNumberDirectory(db)
	got, err := directory.FindIdentities(context.Background(), ws, []string{"5584994409684"})
	if err != nil {
		t.Fatalf("FindIdentities: %v", err)
	}
	if len(got) != 1 || got[0].ID != owner || !got[0].Blocked || !got[0].HoldsIdentity("5584994409684") {
		t.Fatalf("FindIdentities = %+v, want only the live WhatsApp owner of this workspace", got)
	}

	loaded, err := directory.LoadForDial(context.Background(), ws, relative)
	if err != nil {
		t.Fatalf("LoadForDial: %v", err)
	}
	if !loaded.HoldsNumber("558494409684") || loaded.HoldsIdentity("5584994409684") {
		t.Fatalf("LoadForDial = %+v, want the contact phone held but not as WhatsApp", loaded)
	}
}
