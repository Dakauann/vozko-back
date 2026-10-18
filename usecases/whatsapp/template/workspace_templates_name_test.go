package template_usecase

import (
	"errors"
	"testing"

	domain "vozko/domain/whatsapp/template"
)

type namesStub struct {
	asked [][3]string
	found []*domain.Template
	err   error
}

func (n *namesStub) FindGrantedByName(workspaceID, name, language string) ([]*domain.Template, error) {
	n.asked = append(n.asked, [3]string{workspaceID, name, language})
	return n.found, n.err
}

func TestWorkspaceTemplatesFindByNameAsksForTheFoldedNameInTheWorkspace(t *testing.T) {
	names := &namesStub{found: []*domain.Template{{ID: "t1"}, {ID: "t2"}}}
	uc := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{Names: names})

	got, err := uc.FindByName("ws1", "  Promo_Outubro ", " pt_BR ")
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	if len(names.asked) != 1 || names.asked[0] != [3]string{"ws1", "promo_outubro", "pt_BR"} {
		t.Fatalf("asked %v", names.asked)
	}
}

func TestWorkspaceTemplatesFindByNameWithoutNameOrLanguageFindsNothing(t *testing.T) {
	names := &namesStub{found: []*domain.Template{{ID: "t1"}}}
	uc := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{Names: names})
	for _, in := range [][2]string{{" ", "pt_BR"}, {"promo", " "}} {
		got, err := uc.FindByName("ws1", in[0], in[1])
		if err != nil || len(got) != 0 {
			t.Fatalf("%v: got %v, %v", in, got, err)
		}
	}
	if len(names.asked) != 0 {
		t.Fatalf("asked %v", names.asked)
	}
}

func TestWorkspaceTemplatesFindByNameFailsClosed(t *testing.T) {
	if _, err := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{Names: &namesStub{}}).FindByName(" ", "promo", "pt_BR"); !errors.Is(err, domain.ErrTemplateAccessDenied) {
		t.Fatalf("without workspace: %v", err)
	}
	if _, err := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{}).FindByName("ws1", "promo", "pt_BR"); !errors.Is(err, domain.ErrTemplateLookupUnavailable) {
		t.Fatalf("without the lookup: %v", err)
	}
	down := errors.New("database down")
	if _, err := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{Names: &namesStub{err: down}}).FindByName("ws1", "promo", "pt_BR"); !errors.Is(err, down) {
		t.Fatalf("lookup error: %v", err)
	}
}
