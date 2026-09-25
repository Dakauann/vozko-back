package opportunity_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
)

type dealAccess bool

func (a dealAccess) CanAccessEntry(_, _, _, _ string, _ bool) bool { return bool(a) }

type dealScoper struct {
	allowed bool
	scope   conversation.DepartmentAccessScope
}

func (s dealScoper) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return s.scope, s.allowed
}

func TestPersonDealsCreateRefusesAConversationThePersonCannotSee(t *testing.T) {
	repo := newFakeOppRepo()
	deals := NewPersonDeals(newService(repo), dealAccess(false), dealScoper{allowed: true})
	_, err := deals.Create(shared.Person{UserID: "u1"}, "ws1", opportunity.DealDraft{PipelineID: "pipe1", StageID: "stage1", Title: "x", EntryID: "entry-1", EntryType: "whatsapp"})
	if !errors.Is(err, opportunity.ErrEntryAccess) || len(repo.links) != 0 {
		t.Fatalf("err %v links %v", err, repo.links)
	}
}

func TestPersonDealsCreateRecordsThePersonAsCreator(t *testing.T) {
	repo := newFakeOppRepo()
	deals := NewPersonDeals(newAutomationService(repo), dealAccess(true), dealScoper{allowed: true})
	o, err := deals.Create(shared.Person{UserID: "u1"}, "ws1", opportunity.DealDraft{PipelineID: "pipe1", StageID: "stage1", Title: "Plano anual"})
	if err != nil {
		t.Fatal(err)
	}
	if o.CreatedBy != "u1" || o.OwnerID != "u1" {
		t.Fatalf("created by %q owner %q", o.CreatedBy, o.OwnerID)
	}
}

func TestPersonDealsLinkRefusesAConversationThePersonCannotSee(t *testing.T) {
	repo := newFakeOppRepo()
	svc := newAutomationService(repo)
	o, err := svc.Create("ws1", CreateInput{PipelineID: "pipe1", StageID: "stage1", Title: "x", Actor: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewPersonDeals(svc, dealAccess(false), dealScoper{allowed: true}).Link(shared.Person{UserID: "u1"}, "ws1", o.ID, "entry-1", "whatsapp"); !errors.Is(err, opportunity.ErrEntryAccess) {
		t.Fatalf("err = %v", err)
	}
}

func TestPersonDealsListFailsClosedWithoutAScope(t *testing.T) {
	deals := NewPersonDeals(newService(newFakeOppRepo()), dealAccess(true), nil)
	if _, err := deals.ListByPipeline(shared.Person{UserID: "u1"}, "ws1", "pipe1"); !errors.Is(err, opportunity.ErrScopeDenied) {
		t.Fatalf("err = %v", err)
	}
	denied := NewPersonDeals(newService(newFakeOppRepo()), dealAccess(true), dealScoper{allowed: false})
	if _, err := denied.ListByPipeline(shared.Person{UserID: "u1"}, "ws1", "pipe1"); !errors.Is(err, opportunity.ErrScopeDenied) {
		t.Fatalf("err = %v", err)
	}
}
