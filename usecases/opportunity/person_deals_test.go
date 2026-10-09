package opportunity_usecase

import (
	"errors"
	"testing"
	"time"

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

func TestPersonDealsCreateCarriesTheWholeDraft(t *testing.T) {
	repo := newFakeOppRepo()
	deals := NewPersonDeals(newService(repo), dealAccess(true), dealScoper{allowed: true})
	closed := fixedNow.Add(-time.Hour)
	o, err := deals.Create(shared.Person{UserID: "u3", SystemAdmin: true}, "ws1", opportunity.DealDraft{
		PipelineID: "pipe1", StageID: "stage1", Title: "Plano anual", ValueCents: 490000,
		OwnerID: "u2", CarteiraID: "cart-1", Source: "indicacao", CloseDate: &closed,
		CustomFields: map[string]any{"segmento": "enterprise", "score": float64(87)},
		EntryID:      "entry-1", EntryType: "whatsapp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.OwnerID != "u2" || o.CreatedBy != "u3" || o.CarteiraID != "cart-1" || o.Source != "indicacao" || o.CustomFields["segmento"] != "enterprise" || len(repo.links) != 1 {
		t.Fatalf("created %+v links %v", o, repo.links)
	}
}

func TestPersonDealsCreateKeepsTheOwnerChoiceRule(t *testing.T) {
	deals := NewPersonDeals(newService(newFakeOppRepo()), dealAccess(true), dealScoper{allowed: true})
	_, err := deals.Create(shared.Person{UserID: "u3"}, "ws1", opportunity.DealDraft{
		PipelineID: "pipe1", StageID: "stage1", Title: "x", OwnerID: "u2",
		CustomFields: map[string]any{"segmento": "enterprise", "score": float64(87)},
	})
	if !errors.Is(err, ErrOwnerChoiceDenied) {
		t.Fatalf("err = %v, want ErrOwnerChoiceDenied", err)
	}
}

func TestPersonDealsCreateRefusesALinkWithoutAnAccessChecker(t *testing.T) {
	deals := NewPersonDeals(newService(newFakeOppRepo()), nil, dealScoper{allowed: true})
	_, err := deals.Create(shared.Person{UserID: "u1"}, "ws1", opportunity.DealDraft{PipelineID: "pipe1", StageID: "stage1", Title: "x", EntryID: "entry-1", EntryType: "whatsapp"})
	if !errors.Is(err, opportunity.ErrEntryAccess) {
		t.Fatalf("err = %v, want ErrEntryAccess", err)
	}
}

func TestPersonDealsCreateAnswersAMissingEntryTypeAsAShapeError(t *testing.T) {
	repo := newFakeOppRepo()
	deals := NewPersonDeals(newService(repo), dealAccess(true), dealScoper{allowed: true})
	for _, entryType := range []string{"", "  "} {
		_, err := deals.Create(shared.Person{UserID: "u1"}, "ws1", opportunity.DealDraft{PipelineID: "pipe1", StageID: "stage1", Title: "x", EntryID: "entry-1", EntryType: entryType})
		if !errors.Is(err, ErrEntryTypeRequired) || len(repo.links) != 0 {
			t.Fatalf("entry type %q: err %v links %v", entryType, err, repo.links)
		}
	}
}
