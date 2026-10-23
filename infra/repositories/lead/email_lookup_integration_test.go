package lead

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/lead"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestEmailLookupAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "lead_email_lookup")
	migrateLeadTables(t, db)
	if err := db.AutoMigrate(&schema.LeadCompany{}); err != nil {
		t.Fatal(err)
	}
	ws, other := uuid.NewString(), uuid.NewString()
	company := uuid.NewString()
	if err := db.Exec(`INSERT INTO lead_companies (id, workspace_id, name, accepts_leads, created_at, updated_at) VALUES (?, ?, 'Loja Azul', true, now(), now())`, company, ws).Error; err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{"old": uuid.NewString(), "new": uuid.NewString(), "gone": uuid.NewString(), "foreign": uuid.NewString()}
	now := time.Now().UTC()
	rows := []struct {
		key, workspace, email string
		created               time.Time
		deleted               *time.Time
		companyID             any
		optedOut              *time.Time
		blocked               bool
	}{
		{key: "old", workspace: ws, email: "Ana@Example.com", created: now.Add(-time.Hour), companyID: company, optedOut: &now},
		{key: "new", workspace: ws, email: "ana@example.com", created: now, blocked: true},
		{key: "gone", workspace: ws, email: "bia@example.com", created: now, deleted: &now},
		{key: "foreign", workspace: other, email: "bia@example.com", created: now},
	}
	for _, r := range rows {
		if err := db.Exec(`INSERT INTO leads (id, workspace_id, name, email, company_id, opted_out_at, blocked, custom_fields, version, created_at, updated_at, deleted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, '{"plano":"Ouro"}', 1, ?, ?, ?)`,
			ids[r.key], r.workspace, "Lead "+r.key, r.email, r.companyID, r.optedOut, r.blocked, r.created, r.created, r.deleted).Error; err != nil {
			t.Fatalf("insert %s: %v", r.key, err)
		}
	}
	repo := NewRepository(db)
	found, err := repo.FindByEmail(ws, "  ANA@example.COM ")
	if err != nil || found.ID != ids["old"] {
		t.Fatalf("FindByEmail() = %+v, %v; want the oldest lead with that address", found, err)
	}
	if _, err := repo.FindByEmail(ws, "bia@example.com"); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("deleted and foreign leads are not found, err = %v", err)
	}
	if _, err := repo.FindByEmail(ws, "not an email"); !errors.Is(err, lead.ErrLeadInvalid) {
		t.Fatalf("err = %v", err)
	}

	facts, err := NewEmailFacts(db).EmailFacts(context.Background(), ws, []string{ids["old"], ids["new"], ids["gone"], ids["foreign"]})
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("facts = %+v, want only the two live leads of the workspace", facts)
	}
	old := facts[ids["old"]]
	if old.Company != "Loja Azul" || !old.OptedOut || old.Blocked || old.CustomFields["plano"] != "Ouro" || old.Email != "Ana@Example.com" {
		t.Fatalf("old = %+v", old)
	}
	if n := facts[ids["new"]]; !n.Blocked || n.Company != "" {
		t.Fatalf("new = %+v", n)
	}
}
