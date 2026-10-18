package whatsapp_repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/whatsapp/template"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestTemplateEditsAreReadBackWithinTheWindowOnPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "templateedits", &schema.WhatsAppTemplateEdit{})
	log := NewTemplateEditLog(db)
	templateID, other := uuid.NewString(), uuid.NewString()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for _, record := range []template.EditRecord{
		{TemplateID: templateID, WorkspaceID: uuid.NewString(), UserID: "u1", At: now.Add(-40 * 24 * time.Hour)},
		{TemplateID: templateID, WorkspaceID: uuid.NewString(), UserID: "u1", At: now.Add(-2 * time.Hour), Before: []template.TemplateComponent{{Type: "BODY", Text: "Oi"}}},
		{TemplateID: other, WorkspaceID: uuid.NewString(), UserID: "u2", At: now.Add(-time.Hour)},
	} {
		if err := log.Record(record); err != nil {
			t.Fatal(err)
		}
	}

	got, err := log.EditedSince(templateID, now.Add(-30*24*time.Hour))

	if err != nil || len(got) != 1 || !got[0].Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("got %v, %v", got, err)
	}
}
