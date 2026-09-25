package template_usecase

import (
	"errors"
	"testing"

	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	domain "vozko/domain/whatsapp/template"
	"vozko/domain/workspace_template_access"
)

type accessStub struct {
	ids     []string
	granted map[string]bool
}

func (a accessStub) GetTemplateIDsForWorkspace(string) ([]string, error) { return a.ids, nil }
func (a accessStub) HasAccess(_, templateID string) (bool, error)        { return a.granted[templateID], nil }

type listStub struct{ inputs []domain.ListInput }

func (l *listStub) Execute(in domain.ListInput) (*shared.PaginatedResult[*domain.Template], error) {
	l.inputs = append(l.inputs, in)
	return &shared.PaginatedResult[*domain.Template]{Items: []*domain.Template{{ID: "t1"}}}, nil
}

type getStub struct{ asked []string }

func (g *getStub) Execute(id string) (*domain.Template, error) {
	g.asked = append(g.asked, id)
	return &domain.Template{ID: id}, nil
}

func TestWorkspaceTemplatesListsOnlyWhatTheWorkspaceWasGranted(t *testing.T) {
	list := &listStub{}
	uc := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{Access: accessStub{ids: []string{"t1", "t2"}}, List: list, Get: &getStub{}})
	if _, err := uc.List("ws1", domain.ListInput{TemplateIDs: []string{"t9"}}); err != nil {
		t.Fatal(err)
	}
	if got := list.inputs[0].TemplateIDs; len(got) != 2 || got[0] != "t1" {
		t.Fatalf("template ids = %v; the caller's own ids must be replaced by the workspace's", got)
	}
}

func TestWorkspaceTemplatesListIsEmptyWithoutGrants(t *testing.T) {
	list := &listStub{}
	out, err := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{Access: accessStub{}, List: list, Get: &getStub{}}).List("ws1", domain.ListInput{})
	if err != nil || len(out.Items) != 0 || len(list.inputs) != 0 {
		t.Fatalf("out %+v err %v lists %d; no grant must never mean every template", out, err, len(list.inputs))
	}
}

func TestWorkspaceTemplatesGetRefusesATemplateOfAnotherWorkspace(t *testing.T) {
	get := &getStub{}
	uc := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{Access: accessStub{granted: map[string]bool{"t1": true}}, List: &listStub{}, Get: get})
	if _, err := uc.Get("ws1", "t2"); !errors.Is(err, domain.ErrTemplateAccessDenied) || len(get.asked) != 0 {
		t.Fatalf("err = %v after %v", err, get.asked)
	}
	if tmpl, err := uc.Get("ws1", "t1"); err != nil || tmpl.ID != "t1" {
		t.Fatalf("granted template: %v %v", tmpl, err)
	}
}

func TestWorkspaceTemplatesRequireAWorkspace(t *testing.T) {
	uc := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{Access: accessStub{ids: []string{"t1"}, granted: map[string]bool{"t1": true}}, List: &listStub{}, Get: &getStub{}})
	if _, err := uc.List("", domain.ListInput{}); !errors.Is(err, domain.ErrTemplateAccessDenied) {
		t.Fatalf("list err = %v", err)
	}
	if _, err := uc.Get("", "t1"); !errors.Is(err, domain.ErrTemplateAccessDenied) {
		t.Fatalf("get err = %v", err)
	}
}

type phonesStub map[string]string

func (p phonesStub) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	owner, ok := p[id]
	if !ok {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	return &businessphone.WhatsAppBusinessPhoneNumber{ID: id, OwnerWorkspaceID: owner}, nil
}

type grantsStub struct{ granted []string }

func (g *grantsStub) Create(a *workspace_template_access.WorkspaceTemplateAccess) error {
	g.granted = append(g.granted, a.WorkspaceID+"|"+a.TemplateID+"|"+a.GrantedBy)
	return nil
}

type createStub struct{ calls int }

func (c *createStub) Execute(in domain.CreateTemplateInput) (*domain.CreateTemplateOutput, error) {
	c.calls++
	return &domain.CreateTemplateOutput{ID: "t-new", Name: in.Name}, nil
}

func TestWorkspaceTemplatesCreateRefusesAnotherWorkspacesPhone(t *testing.T) {
	create, grants := &createStub{}, &grantsStub{}
	uc := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{Phones: phonesStub{"p1": "ws2"}, Grants: grants, Create: create})
	if _, err := uc.Create("ws1", "u1", domain.CreateTemplateInput{BusinessPhoneID: "p1", Name: "x"}); !errors.Is(err, domain.ErrPhoneOutsideWorkspace) || create.calls != 0 {
		t.Fatalf("err %v creates %d", err, create.calls)
	}
}

func TestWorkspaceTemplatesCreateGrantsTheNewTemplateToTheWorkspace(t *testing.T) {
	create, grants := &createStub{}, &grantsStub{}
	uc := NewWorkspaceTemplatesUseCase(WorkspaceTemplatesDeps{Phones: phonesStub{"p1": "ws1"}, Grants: grants, Create: create})
	if _, err := uc.Create("ws1", "u1", domain.CreateTemplateInput{BusinessPhoneID: "p1", Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(grants.granted) != 1 || grants.granted[0] != "ws1|t-new|u1" {
		t.Fatalf("granted %v", grants.granted)
	}
}
