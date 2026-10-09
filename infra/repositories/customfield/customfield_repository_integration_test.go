package customfield_repository

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/customfield"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestDefinitionsKeepTheirSensitivityTonesAndRole(t *testing.T) {
	db := repotest.IsolatedDB(t, "cfrepo", &schema.CustomFieldDefinition{})
	repo := NewRepository(db)
	ws := uuid.New().String()

	d := &customfield.Definition{
		ID: uuid.New().String(), WorkspaceID: ws, ObjectType: customfield.ObjectLead,
		Key: "classificacao", Label: "Classificação", Type: customfield.TypeSelect,
		Options:     []string{"Positivo", "Negativo"},
		OptionTones: map[string]customfield.Tone{"Positivo": customfield.ToneChart1, "Negativo": customfield.ToneNeutral},
		Sensitive:   true, LegalBasis: "Consentimento", Role: customfield.RoleClassification,
	}
	if err := repo.Create(d); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, err := repo.GetByID(ws, d.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if !got.Sensitive || got.LegalBasis != "Consentimento" || got.Role != customfield.RoleClassification || !reflect.DeepEqual(got.OptionTones, d.OptionTones) {
		t.Fatalf("GetByID() = %+v", got)
	}

	got.Sensitive = false
	got.LegalBasis = ""
	got.Role = ""
	got.OptionTones = nil
	if err := repo.Update(got); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	listed, err := repo.ListByObject(ws, customfield.ObjectLead)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListByObject() = %v, %v", listed, err)
	}
	if listed[0].Sensitive || listed[0].Role != "" || listed[0].OptionTones != nil {
		t.Fatalf("Update() did not clear the properties: %+v", listed[0])
	}
}

func TestLegacyDefinitionsWithoutTheNewColumnsStillLoad(t *testing.T) {
	db := repotest.IsolatedDB(t, "cfrepo", &schema.CustomFieldDefinition{})
	ws := uuid.New().String()
	id := uuid.New().String()
	if err := db.Exec(`INSERT INTO custom_field_definitions (id, workspace_id, object_type, key, label, type, created_at, updated_at)
		VALUES (?, ?, 'opportunity', 'origem', 'Origem', 'text', NOW(), NOW())`, id, ws).Error; err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	got, err := NewRepository(db).GetByID(ws, id)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Sensitive || got.LegalBasis != "" || got.Role != "" || got.OptionTones != nil {
		t.Fatalf("a legacy row must load as a plain field: %+v", got)
	}
}

func TestAMissingDefinitionIsTheDomainNotFound(t *testing.T) {
	db := repotest.IsolatedDB(t, "cfrepo", &schema.CustomFieldDefinition{})
	if _, err := NewRepository(db).GetByID(uuid.New().String(), uuid.New().String()); !errors.Is(err, customfield.ErrNotFound) {
		t.Fatalf("GetByID() error = %v, want customfield.ErrNotFound", err)
	}
}

func TestADeletedDefinitionStaysInTheKeyHistory(t *testing.T) {
	db := repotest.IsolatedDB(t, "cfrepo", &schema.CustomFieldDefinition{})
	repo := NewRepository(db)
	ws := uuid.New().String()
	define := func(workspaceID string, object customfield.ObjectType, key string, sensitive bool) *customfield.Definition {
		d := &customfield.Definition{ID: uuid.New().String(), WorkspaceID: workspaceID, ObjectType: object, Key: key, Label: key, Type: customfield.TypeText, Sensitive: sensitive}
		if sensitive {
			d.LegalBasis = "Consentimento"
		}
		if err := repo.Create(d); err != nil {
			t.Fatalf("Create(%s) error = %v", key, err)
		}
		return d
	}

	sensitive := define(ws, customfield.ObjectLead, "classificacao", true)
	define(ws, customfield.ObjectLead, "cor", false)
	define(uuid.New().String(), customfield.ObjectLead, "classificacao", true)
	if err := repo.Delete(ws, sensitive.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if live, err := repo.ListByObject(ws, customfield.ObjectLead); err != nil || len(live) != 1 || live[0].Key != "cor" {
		t.Fatalf("ListByObject() after delete = %v, %v", live, err)
	}
	recreated := define(ws, customfield.ObjectLead, "classificacao", false)

	retired, err := repo.RetiredByKey(ws, customfield.ObjectLead, "classificacao")
	if err != nil {
		t.Fatalf("RetiredByKey() error = %v", err)
	}
	if len(retired) != 1 || retired[0].ID != sensitive.ID || !retired[0].Sensitive {
		t.Fatalf("RetiredByKey() = %+v, want only the deleted sensitive definition", retired)
	}
	if retired[0].ID == recreated.ID {
		t.Fatalf("RetiredByKey() returned the live definition")
	}
	for _, probe := range []struct {
		object customfield.ObjectType
		key    string
	}{{customfield.ObjectLead, "cor"}, {customfield.ObjectOpportunity, "classificacao"}} {
		if got, err := repo.RetiredByKey(ws, probe.object, probe.key); err != nil || len(got) != 0 {
			t.Fatalf("RetiredByKey(%s, %s) = %v, %v; want none", probe.object, probe.key, got, err)
		}
	}
}
