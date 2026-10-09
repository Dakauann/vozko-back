package customfield_repository

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/customfield"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestARaceOnTheClassificationRoleEndsAsRoleTakenAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "cfrole_repo", &schema.CustomFieldDefinition{})
	for _, name := range []string{schema.CustomFieldLiveKeyIndex, schema.CustomFieldLiveRoleIndex} {
		sql, ok := database.ConcurrentIndexSQL(name)
		if !ok {
			t.Fatalf("%s is not declared", name)
		}
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	repo := NewRepository(db)
	ws := uuid.New().String()
	definition := func(key string, role customfield.Role) *customfield.Definition {
		return &customfield.Definition{
			ID: uuid.New().String(), WorkspaceID: ws, ObjectType: customfield.ObjectLead,
			Key: key, Label: key, Type: customfield.TypeSelect, Options: []string{"Positivo", "Negativo"}, Role: role,
		}
	}

	if err := repo.Create(definition("classificacao", customfield.RoleClassification)); err != nil {
		t.Fatalf("first classification: %v", err)
	}
	if err := repo.Create(definition("interesse", customfield.RoleClassification)); !errors.Is(err, customfield.ErrRoleTaken) {
		t.Fatalf("Create() of a second classification = %v, want ErrRoleTaken", err)
	}
	if err := repo.Create(definition("classificacao", "")); !errors.Is(err, customfield.ErrKeyExists) {
		t.Fatalf("Create() of a taken key = %v, want ErrKeyExists", err)
	}
	plain := definition("origem", "")
	if err := repo.Create(plain); err != nil {
		t.Fatalf("a field without a role: %v", err)
	}
	plain.Role = customfield.RoleClassification
	if err := repo.Update(plain); !errors.Is(err, customfield.ErrRoleTaken) {
		t.Fatalf("Update() onto a taken role = %v, want ErrRoleTaken", err)
	}
}
