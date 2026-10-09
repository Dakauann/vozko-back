package customfield_usecase

import (
	"errors"
	"testing"

	"vozko/domain/customfield"
)

type fakeRepo struct {
	defs       map[string]*customfield.Definition
	retired    []*customfield.Definition
	listErr    error
	historyErr error
}

func newFakeRepo(defs ...*customfield.Definition) *fakeRepo {
	r := &fakeRepo{defs: map[string]*customfield.Definition{}}
	for _, d := range defs {
		r.defs[d.ID] = d
	}
	return r
}

func (r *fakeRepo) Create(d *customfield.Definition) error {
	copied := *d
	r.defs[d.ID] = &copied
	return nil
}

func (r *fakeRepo) Update(d *customfield.Definition) error {
	if _, ok := r.defs[d.ID]; !ok {
		return customfield.ErrNotFound
	}
	copied := *d
	r.defs[d.ID] = &copied
	return nil
}

func (r *fakeRepo) Delete(workspaceID, id string) error {
	if d, ok := r.defs[id]; ok && d.WorkspaceID == workspaceID {
		r.retired = append(r.retired, d)
	}
	delete(r.defs, id)
	return nil
}

func (r *fakeRepo) RetiredByKey(workspaceID string, objectType customfield.ObjectType, key string) ([]*customfield.Definition, error) {
	if r.historyErr != nil {
		return nil, r.historyErr
	}
	var out []*customfield.Definition
	for _, d := range r.retired {
		if d.WorkspaceID == workspaceID && d.ObjectType == objectType && d.Key == key {
			copied := *d
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *fakeRepo) GetByID(workspaceID, id string) (*customfield.Definition, error) {
	d, ok := r.defs[id]
	if !ok || d.WorkspaceID != workspaceID {
		return nil, customfield.ErrNotFound
	}
	copied := *d
	return &copied, nil
}

func (r *fakeRepo) ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*customfield.Definition
	for _, d := range r.defs {
		if d.WorkspaceID == workspaceID && d.ObjectType == objectType {
			copied := *d
			out = append(out, &copied)
		}
	}
	return out, nil
}

func boolPtr(v bool) *bool { return &v }

func classificationInput() CreateInput {
	return CreateInput{
		ObjectType:  customfield.ObjectLead,
		Key:         "classificacao",
		Label:       "Classificação",
		Type:        customfield.TypeSelect,
		Options:     []string{"Positivo", "Negativo"},
		OptionTones: map[string]customfield.Tone{"Positivo": customfield.ToneChart1, "Negativo": customfield.ToneChart3},
		Sensitive:   boolPtr(true),
		LegalBasis:  "Consentimento",
		Role:        customfield.RoleClassification,
	}
}

func TestCreateKeepsTheNewProperties(t *testing.T) {
	svc := NewService(newFakeRepo(), grantAll())
	created, err := svc.Create(member("ws1"), classificationInput())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created.Sensitive || created.LegalBasis != "Consentimento" || created.Role != customfield.RoleClassification {
		t.Fatalf("Create() = %+v", created)
	}
	if created.OptionTones["Negativo"] != customfield.ToneChart3 {
		t.Fatalf("OptionTones = %v", created.OptionTones)
	}
}

func TestCreateRefusals(t *testing.T) {
	existingClassification := &customfield.Definition{
		ID: "c1", WorkspaceID: "ws1", ObjectType: customfield.ObjectLead, Key: "posicao",
		Label: "Posição", Type: customfield.TypeSelect, Options: []string{"a"}, Role: customfield.RoleClassification,
	}
	existingKey := &customfield.Definition{
		ID: "k1", WorkspaceID: "ws1", ObjectType: customfield.ObjectLead, Key: "classificacao",
		Label: "Old", Type: customfield.TypeText,
	}

	cases := []struct {
		name   string
		repo   *fakeRepo
		mutate func(*CreateInput)
		want   error
	}{
		{"lead field without a sensitive choice", newFakeRepo(), func(in *CreateInput) { in.Sensitive = nil }, customfield.ErrSensitivityChoiceMissing},
		{"unknown object", newFakeRepo(), func(in *CreateInput) { in.ObjectType = "conversation" }, customfield.ErrInvalidObjectType},
		{"sensitive without legal basis", newFakeRepo(), func(in *CreateInput) { in.LegalBasis = "" }, customfield.ErrLegalBasisRequired},
		{"duplicate key", newFakeRepo(existingKey), func(in *CreateInput) {}, customfield.ErrKeyExists},
		{"second classification", newFakeRepo(existingClassification), func(in *CreateInput) {}, customfield.ErrRoleTaken},
		{"definitions unavailable", &fakeRepo{defs: map[string]*customfield.Definition{}, listErr: errors.New("db down")}, func(in *CreateInput) {}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := classificationInput()
			tc.mutate(&in)
			_, err := NewService(tc.repo, grantAll()).Create(member("ws1"), in)
			if tc.want == nil {
				if err == nil {
					t.Fatal("Create() must refuse when it cannot check the existing fields")
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Create() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAnOpportunityFieldDefaultsToNotSensitive(t *testing.T) {
	created, err := NewService(newFakeRepo(), grantAll()).Create(member("ws1"), CreateInput{
		ObjectType: customfield.ObjectOpportunity, Key: "origem", Label: "Origem", Type: customfield.TypeText,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Sensitive {
		t.Fatal("an opportunity field must default to not sensitive")
	}
}

func TestTheClassificationRoleIsPerObject(t *testing.T) {
	repo := newFakeRepo(&customfield.Definition{
		ID: "o1", WorkspaceID: "ws1", ObjectType: customfield.ObjectOpportunity, Key: "classificacao",
		Label: "Classe", Type: customfield.TypeSelect, Options: []string{"a"}, Role: customfield.RoleClassification,
	})
	if _, err := NewService(repo, grantAll()).Create(member("ws1"), classificationInput()); err != nil {
		t.Fatalf("a lead classification must coexist with an opportunity one: %v", err)
	}
}

func TestUpdateRules(t *testing.T) {
	base := func() *fakeRepo {
		return newFakeRepo(
			&customfield.Definition{
				ID: "c1", WorkspaceID: "ws1", ObjectType: customfield.ObjectLead, Key: "classificacao",
				Label: "Classificação", Type: customfield.TypeSelect, Options: []string{"Positivo", "Negativo"},
				OptionTones: map[string]customfield.Tone{"Positivo": customfield.ToneChart1, "Negativo": customfield.ToneChart3},
				Sensitive:   true, LegalBasis: "Consentimento", Role: customfield.RoleClassification,
			},
			&customfield.Definition{
				ID: "s1", WorkspaceID: "ws1", ObjectType: customfield.ObjectLead, Key: "segmento",
				Label: "Segmento", Type: customfield.TypeSelect, Options: []string{"a"},
			},
		)
	}
	classification := customfield.RoleClassification
	empty := ""

	t.Run("keeping its own role", func(t *testing.T) {
		label := "Nova"
		if _, err := NewService(base(), grantAll()).Update(member("ws1"), "c1", UpdateInput{Label: &label, Role: &classification}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
	})
	t.Run("taking a role another field holds", func(t *testing.T) {
		if _, err := NewService(base(), grantAll()).Update(member("ws1"), "s1", UpdateInput{Role: &classification}); !errors.Is(err, customfield.ErrRoleTaken) {
			t.Fatalf("Update() error = %v, want ErrRoleTaken", err)
		}
	})
	t.Run("removing an option drops its tone", func(t *testing.T) {
		updated, err := NewService(base(), grantAll()).Update(member("ws1"), "c1", UpdateInput{Options: []string{"Positivo"}})
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if _, kept := updated.OptionTones["Negativo"]; kept {
			t.Fatalf("OptionTones = %v", updated.OptionTones)
		}
	})
	t.Run("replacing tones", func(t *testing.T) {
		updated, err := NewService(base(), grantAll()).Update(member("ws1"), "c1", UpdateInput{OptionTones: map[string]customfield.Tone{"Positivo": customfield.ToneChart5}})
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if len(updated.OptionTones) != 1 || updated.OptionTones["Positivo"] != customfield.ToneChart5 {
			t.Fatalf("OptionTones = %v", updated.OptionTones)
		}
	})
	t.Run("clearing the legal basis of a sensitive field", func(t *testing.T) {
		if _, err := NewService(base(), grantAll()).Update(member("ws1"), "c1", UpdateInput{LegalBasis: &empty}); !errors.Is(err, customfield.ErrLegalBasisRequired) {
			t.Fatalf("Update() error = %v, want ErrLegalBasisRequired", err)
		}
	})
	t.Run("turning sensitivity off", func(t *testing.T) {
		updated, err := NewService(base(), grantAll()).Update(member("ws1"), "c1", UpdateInput{Sensitive: boolPtr(false)})
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if updated.Sensitive || updated.LegalBasis != "" {
			t.Fatalf("Update() = %+v", updated)
		}
	})
	t.Run("dropping the role", func(t *testing.T) {
		none := customfield.Role("")
		updated, err := NewService(base(), grantAll()).Update(member("ws1"), "c1", UpdateInput{Role: &none})
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if updated.Role != "" {
			t.Fatalf("Role = %q", updated.Role)
		}
	})
}

func TestListByObjectRefusesAnUnknownObject(t *testing.T) {
	if _, err := NewService(newFakeRepo(), grantAll()).ListByObject(member("ws1"), "conversation"); !errors.Is(err, customfield.ErrInvalidObjectType) {
		t.Fatalf("ListByObject() error = %v, want ErrInvalidObjectType", err)
	}
}
