package whatsapp_repository

import (
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestGrantedTemplatesByNameAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "template_name_lookup", &schema.WhatsAppTemplate{})
	if err := db.Exec(`CREATE TABLE workspace_template_access (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, template_id uuid NOT NULL,
		granted_by uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE (workspace_id, template_id))`).Error; err != nil {
		t.Fatalf("access table: %v", err)
	}
	ws, other := uuid.NewString(), uuid.NewString()

	insert := func(name, language, waba string, deleted bool, grantedTo ...string) string {
		t.Helper()
		row := schema.WhatsAppTemplate{ID: uuid.NewString(), ExternalID: uuid.NewString(), WABAId: waba, Name: name, Language: language, Category: "MARKETING", Status: "APPROVED"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
		for _, workspace := range grantedTo {
			if err := db.Exec(`INSERT INTO workspace_template_access (id, workspace_id, template_id, granted_by) VALUES (?, ?, ?, ?)`,
				uuid.NewString(), workspace, row.ID, uuid.NewString()).Error; err != nil {
				t.Fatalf("grant %s: %v", name, err)
			}
		}
		if deleted {
			if err := db.Model(&schema.WhatsAppTemplate{}).Where("id = ?", row.ID).Update("deleted_at", time.Now()).Error; err != nil {
				t.Fatalf("delete %s: %v", name, err)
			}
		}
		return row.ID
	}

	for i := 0; i < 120; i++ {
		insert("promo_outubro_"+uuid.NewString()[:6], "pt_BR", "waba-1", false, ws)
	}
	first := insert("promo_outubro", "pt_BR", "waba-1", false, ws)
	second := insert("Promo_Outubro", "pt_BR", "waba-2", false, ws, other)
	insert("promo_outubro", "en_US", "waba-1", false, ws)
	insert("promo_outubro", "pt_BR", "waba-3", false, other)
	insert("promo_outubro", "pt_BR", "waba-4", true, ws)
	insert("promo_outubro", "pt_BR", "waba-5", false)

	got, err := NewGrantedTemplateNames(db).FindGrantedByName(ws, "promo_outubro", "pt_BR")
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(got))
	for _, tmpl := range got {
		ids = append(ids, tmpl.ID)
	}
	want := []string{first, second}
	sort.Strings(ids)
	sort.Strings(want)
	if len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("found %v, want %v: the exact name in any case, the asked language, granted to the workspace and not deleted", ids, want)
	}

	none, err := NewGrantedTemplateNames(db).FindGrantedByName(ws, "promo", "pt_BR")
	if err != nil || len(none) != 0 {
		t.Fatalf("a substring is not the name: %v, %v", none, err)
	}
}
