package customfield_usecase

import (
	"errors"
	"testing"

	"vozko/domain/customfield"
)

type grants map[string]bool

func (g grants) HasWorkspacePermission(_, _, resource, action string, _ bool) bool {
	return g[resource+":"+action]
}

func grantAll() grants {
	return grants{
		"leads:read": true, "leads:configure": true, "leads:read_sensitive": true,
		"conversations:read": true, "conversations:create": true, "conversations:update": true, "conversations:delete": true,
	}
}

func member(workspaceID string) Actor {
	return Actor{UserID: "4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00", WorkspaceID: workspaceID}
}

func definitionsOfBothObjects() *fakeRepo {
	return newFakeRepo(
		&customfield.Definition{ID: "lead-1", WorkspaceID: "ws1", ObjectType: customfield.ObjectLead, Key: "cor", Label: "Cor", Type: customfield.TypeText},
		&customfield.Definition{ID: "opp-1", WorkspaceID: "ws1", ObjectType: customfield.ObjectOpportunity, Key: "valor", Label: "Valor", Type: customfield.TypeNumber},
	)
}

func TestAConversationsUpdaterCannotPatchALeadDefinition(t *testing.T) {
	label := "Nova"
	svc := NewService(definitionsOfBothObjects(), grants{"conversations:read": true, "conversations:update": true})
	if _, err := svc.Update(member("ws1"), "lead-1", UpdateInput{Label: &label}); !errors.Is(err, customfield.ErrForbidden) {
		t.Fatalf("Update(lead) = %v, want ErrForbidden", err)
	}
	if _, err := svc.Update(member("ws1"), "opp-1", UpdateInput{Label: &label}); err != nil {
		t.Fatalf("Update(opportunity) = %v, the opportunity rule is unchanged", err)
	}
}

func TestLeadDefinitionsFollowTheLeadPermissions(t *testing.T) {
	configurer := grants{"leads:read": true, "leads:configure": true}
	reader := grants{"leads:read": true}
	sensitive := false
	newLead := CreateInput{ObjectType: customfield.ObjectLead, Key: "bairro_origem", Label: "Bairro de origem", Type: customfield.TypeText, Sensitive: &sensitive}
	newOpportunity := CreateInput{ObjectType: customfield.ObjectOpportunity, Key: "canal", Label: "Canal", Type: customfield.TypeText}

	cases := []struct {
		name  string
		perms grants
		run   func(*Service) error
		allow bool
	}{
		{"a configurer creates a lead field", configurer, func(s *Service) error { _, err := s.Create(member("ws1"), newLead); return err }, true},
		{"a reader cannot create a lead field", reader, func(s *Service) error { _, err := s.Create(member("ws1"), newLead); return err }, false},
		{"a lead configurer cannot create an opportunity field", configurer, func(s *Service) error { _, err := s.Create(member("ws1"), newOpportunity); return err }, false},
		{"a reader lists lead fields", reader, func(s *Service) error { _, err := s.ListByObject(member("ws1"), customfield.ObjectLead); return err }, true},
		{"a reader cannot list opportunity fields", reader, func(s *Service) error {
			_, err := s.ListByObject(member("ws1"), customfield.ObjectOpportunity)
			return err
		}, false},
		{"a reader gets a lead field", reader, func(s *Service) error { _, err := s.Get(member("ws1"), "lead-1"); return err }, true},
		{"a reader cannot delete a lead field", reader, func(s *Service) error { return s.Delete(member("ws1"), "lead-1") }, false},
		{"a configurer deletes a lead field", configurer, func(s *Service) error { return s.Delete(member("ws1"), "lead-1") }, true},
		{"nobody without permissions reads", grants{}, func(s *Service) error { _, err := s.Get(member("ws1"), "opp-1"); return err }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(NewService(definitionsOfBothObjects(), tc.perms))
			if tc.allow && err != nil {
				t.Fatalf("err = %v, want allowed", err)
			}
			if !tc.allow && !errors.Is(err, customfield.ErrForbidden) {
				t.Fatalf("err = %v, want ErrForbidden", err)
			}
		})
	}
}

func TestTheServiceFailsClosedWithoutACaller(t *testing.T) {
	if _, err := NewService(definitionsOfBothObjects(), nil).Get(member("ws1"), "lead-1"); !errors.Is(err, customfield.ErrForbidden) {
		t.Fatalf("without permissions = %v, want ErrForbidden", err)
	}
	if _, err := NewService(definitionsOfBothObjects(), grantAll()).Get(Actor{WorkspaceID: "ws1"}, "lead-1"); !errors.Is(err, customfield.ErrForbidden) {
		t.Fatalf("without a user = %v, want ErrForbidden", err)
	}
	if _, err := NewService(definitionsOfBothObjects(), grantAll()).Get(member("ws2"), "lead-1"); !errors.Is(err, customfield.ErrNotFound) {
		t.Fatalf("another workspace = %v, want ErrNotFound", err)
	}
}

func TestTheServiceRefusesACallWithoutAWorkspace(t *testing.T) {
	if _, err := NewService(definitionsOfBothObjects(), grantAll()).ListByObject(member(""), customfield.ObjectLead); !errors.Is(err, customfield.ErrWorkspaceRequired) {
		t.Fatalf("without a workspace = %v, want ErrWorkspaceRequired", err)
	}
}

func sensitiveClassification() *fakeRepo {
	return newFakeRepo(&customfield.Definition{
		ID: "class-1", WorkspaceID: "ws1", ObjectType: customfield.ObjectLead, Key: "classificacao", Label: "Classificação",
		Type: customfield.TypeSelect, Options: []string{"Apoiador", "Opositor"}, Sensitive: true, LegalBasis: "Art. 11, II, a",
	})
}

func TestAConfigurerWithoutSensitiveAccessCannotTouchASensitiveField(t *testing.T) {
	configurer := grants{"leads:read": true, "leads:configure": true}
	trusted := grants{"leads:read": true, "leads:configure": true, "leads:read_sensitive": true}
	off, label := false, "Classe"
	text := customfield.TypeText
	legal := "Consentimento"
	on := true
	newSensitive := CreateInput{ObjectType: customfield.ObjectLead, Key: "religiao", Label: "Religião", Type: customfield.TypeText, Sensitive: &on, LegalBasis: "Art. 11, I"}
	plain := func() *fakeRepo {
		return newFakeRepo(&customfield.Definition{ID: "class-1", WorkspaceID: "ws1", ObjectType: customfield.ObjectLead, Key: "cor", Label: "Cor", Type: customfield.TypeText})
	}

	cases := []struct {
		name  string
		repo  func() *fakeRepo
		perms grants
		run   func(*Service) error
		allow bool
	}{
		{"turning the flag off", sensitiveClassification, configurer, func(s *Service) error {
			_, err := s.Update(member("ws1"), "class-1", UpdateInput{Sensitive: &off})
			return err
		}, false},
		{"renaming it", sensitiveClassification, configurer, func(s *Service) error {
			_, err := s.Update(member("ws1"), "class-1", UpdateInput{Label: &label})
			return err
		}, false},
		{"retyping it", sensitiveClassification, configurer, func(s *Service) error {
			_, err := s.Update(member("ws1"), "class-1", UpdateInput{Type: &text})
			return err
		}, false},
		{"changing the legal basis", sensitiveClassification, configurer, func(s *Service) error {
			_, err := s.Update(member("ws1"), "class-1", UpdateInput{LegalBasis: &legal})
			return err
		}, false},
		{"deleting it", sensitiveClassification, configurer, func(s *Service) error { return s.Delete(member("ws1"), "class-1") }, false},
		{"creating one", plain, configurer, func(s *Service) error { _, err := s.Create(member("ws1"), newSensitive); return err }, false},
		{"marking a plain field sensitive", plain, configurer, func(s *Service) error {
			_, err := s.Update(member("ws1"), "class-1", UpdateInput{Sensitive: &on, LegalBasis: &legal})
			return err
		}, false},
		{"reading it", sensitiveClassification, grants{"leads:read": true}, func(s *Service) error {
			_, err := s.Get(member("ws1"), "class-1")
			return err
		}, true},
		{"a trusted configurer turns the flag off", sensitiveClassification, trusted, func(s *Service) error {
			_, err := s.Update(member("ws1"), "class-1", UpdateInput{Sensitive: &off})
			return err
		}, true},
		{"a trusted configurer deletes it", sensitiveClassification, trusted, func(s *Service) error { return s.Delete(member("ws1"), "class-1") }, true},
		{"a trusted configurer creates one", plain, trusted, func(s *Service) error { _, err := s.Create(member("ws1"), newSensitive); return err }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := tc.repo()
			before := *repo.defs["class-1"]
			err := tc.run(NewService(repo, tc.perms))
			if tc.allow {
				if err != nil {
					t.Fatalf("err = %v, want allowed", err)
				}
				return
			}
			if !errors.Is(err, customfield.ErrForbidden) {
				t.Fatalf("err = %v, want ErrForbidden", err)
			}
			if after, ok := repo.defs["class-1"]; !ok || after.Sensitive != before.Sensitive || after.Label != before.Label {
				t.Fatalf("a refused change must leave the field as it was, got %+v", after)
			}
			if len(repo.defs) != 1 {
				t.Fatalf("a refused create must write nothing, got %d definitions", len(repo.defs))
			}
		})
	}
}

func TestCreateChecksThePermissionOfTheNormalizedObject(t *testing.T) {
	plain := false
	for _, object := range []customfield.ObjectType{"LEAD", " Lead "} {
		in := CreateInput{ObjectType: object, Key: "bairro_origem", Label: "Bairro de origem", Type: customfield.TypeText, Sensitive: &plain}
		repo := newFakeRepo()
		_, err := NewService(repo, grants{"conversations:create": true}).Create(member("ws1"), in)
		if !errors.Is(err, customfield.ErrForbidden) {
			t.Fatalf("Create(%q) without leads:configure = %v, want ErrForbidden", object, err)
		}
		if len(repo.defs) != 0 {
			t.Fatalf("Create(%q) wrote %d definitions without permission", object, len(repo.defs))
		}
		if _, err := NewService(newFakeRepo(), grants{"leads:read": true, "leads:configure": true}).Create(member("ws1"), in); err != nil {
			t.Fatalf("Create(%q) by a configurer = %v, want allowed", object, err)
		}
	}
	if _, err := NewService(newFakeRepo(), grantAll()).Create(member("ws1"), CreateInput{ObjectType: "contract", Key: "x", Label: "X", Type: customfield.TypeText}); !errors.Is(err, customfield.ErrInvalidObjectType) {
		t.Fatalf("Create(contract) = %v, want ErrInvalidObjectType", err)
	}
}

func TestAConfigurerCannotReuseTheKeyOfADeletedSensitiveField(t *testing.T) {
	configurer := grants{"leads:read": true, "leads:configure": true}
	trusted := grants{"leads:read": true, "leads:configure": true, "leads:read_sensitive": true}
	plain := false
	reuse := CreateInput{ObjectType: customfield.ObjectLead, Key: "classificacao", Label: "Classificação", Type: customfield.TypeText, Sensitive: &plain}

	repo := sensitiveClassification()
	if err := NewService(repo, trusted).Delete(member("ws1"), "class-1"); err != nil {
		t.Fatalf("Delete by a trusted configurer = %v", err)
	}

	if _, err := NewService(repo, configurer).Create(member("ws1"), reuse); !errors.Is(err, customfield.ErrForbidden) {
		t.Fatalf("Create on a retired sensitive key by a configurer = %v, want ErrForbidden", err)
	}
	if len(repo.defs) != 0 {
		t.Fatalf("a refused reuse must write nothing, got %d definitions", len(repo.defs))
	}

	other := reuse
	other.Key = "cor"
	if _, err := NewService(repo, configurer).Create(member("ws1"), other); err != nil {
		t.Fatalf("Create on an unrelated key = %v, want allowed", err)
	}
	elsewhere := newFakeRepo()
	elsewhere.retired = repo.retired
	if _, err := NewService(elsewhere, configurer).Create(member("ws2"), reuse); err != nil {
		t.Fatalf("Create on the same key in another workspace = %v, want allowed", err)
	}

	if _, err := NewService(repo, trusted).Create(member("ws1"), reuse); err != nil {
		t.Fatalf("Create on a retired sensitive key by a trusted configurer = %v, want allowed", err)
	}
}

func TestCreateRefusesWhenTheKeyHistoryCannotBeRead(t *testing.T) {
	plain := false
	repo := newFakeRepo()
	repo.historyErr = errors.New("db down")
	_, err := NewService(repo, grantAll()).Create(member("ws1"), CreateInput{ObjectType: customfield.ObjectLead, Key: "cor", Label: "Cor", Type: customfield.TypeText, Sensitive: &plain})
	if err == nil || len(repo.defs) != 0 {
		t.Fatalf("Create with an unreadable key history = %v (%d written), want a refusal", err, len(repo.defs))
	}
}
