package database

import (
	"testing"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

var leadIndexNames = []string{
	"ux_leads_workspace_id_id",
	"idx_leads_workspace_owner",
	"idx_leads_workspace_birthday",
	"idx_leads_workspace_referred",
	"idx_leads_workspace_company",
}

func TestLeadIndexesBuildOnTheLeadsTableAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_indexes")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, name := range leadIndexNames {
		idx := declaredConcurrentIndex(t, name)
		if err := createIndexConcurrently(db, idx); err != nil {
			t.Fatalf("%s: %v", idx.name, err)
		}
		var valid bool
		if err := db.Raw(`SELECT COALESCE((SELECT i.indisvalid FROM pg_index i WHERE i.indexrelid = to_regclass(?::text)), false)`, idx.name).Scan(&valid).Error; err != nil || !valid {
			t.Fatalf("%s is not a valid index: %v", idx.name, err)
		}
	}
}

func TestOldLeadsBecomeVersionOneAndTheIdentityTurnsOptionalAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_identity")
	for _, stmt := range []string{
		`CREATE TABLE leads (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, number varchar(20) NOT NULL, name varchar(255),
			profile_picture_url varchar(1024), age int, blocked bool NOT NULL DEFAULT false, blocked_at timestamptz,
			blocked_by uuid, created_at timestamptz, updated_at timestamptz, deleted_at timestamptz)`,
		`CREATE UNIQUE INDEX ux_leads_workspace_number ON leads (workspace_id, number) WHERE deleted_at IS NULL`,
		`INSERT INTO leads (id, workspace_id, number) VALUES (gen_random_uuid(), gen_random_uuid(), '5511987654321')`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.Lead{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var version int64
	if err := db.Raw(`SELECT version FROM leads`).Scan(&version).Error; err != nil || version != 1 {
		t.Fatalf("an existing lead must read as version 1, got %d (%v)", version, err)
	}
	var nullable string
	db.Raw(`SELECT is_nullable FROM information_schema.columns WHERE table_name = 'leads' AND column_name = 'number' AND table_schema = current_schema()`).Scan(&nullable)
	if nullable != "YES" {
		t.Fatalf("leads.number must accept NULL after the migration, is_nullable = %q", nullable)
	}
	ws := `00000000-0000-4000-8000-000000000001`
	for i := 0; i < 2; i++ {
		if err := db.Exec(`INSERT INTO leads (id, workspace_id, number, name) VALUES (gen_random_uuid(), ?, NULL, 'Sem número')`, ws).Error; err != nil {
			t.Fatalf("two leads without identity must coexist under the unchanged unique index: %v", err)
		}
	}
}
