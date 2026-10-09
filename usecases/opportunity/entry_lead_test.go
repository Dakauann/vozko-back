package opportunity_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/lead"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
)

type entryLeads struct {
	byEntry map[string]string
	err     error
	asked   []shared.EntryRef
}

func (e *entryLeads) LeadOfEntry(_ context.Context, workspaceID string, ref shared.EntryRef) (string, error) {
	e.asked = append(e.asked, ref)
	if e.err != nil {
		return "", e.err
	}
	if id, ok := e.byEntry[workspaceID+"/"+ref.EntryID]; ok {
		return id, nil
	}
	return "", lead.ErrLeadNotFound
}

func serviceWithEntryLeads(repo *fakeOppRepo, leads EntryLeads) *Service {
	s := newService(repo)
	s.entryLeads = leads
	return s
}

func TestCreateFromAConversationTakesTheLeadOfThatConversation(t *testing.T) {
	repo := newFakeOppRepo()
	leads := &entryLeads{byEntry: map[string]string{"ws1/entry-1": "lead-7"}}
	in := baseCreate()
	in.LinkEntryID, in.LinkEntryType = "entry-1", "whatsapp"
	o, err := serviceWithEntryLeads(repo, leads).Create("ws1", in)
	if err != nil {
		t.Fatal(err)
	}
	if o.LeadID != "lead-7" {
		t.Fatalf("lead = %q, want the conversation's lead", o.LeadID)
	}
	if len(leads.asked) != 1 || leads.asked[0] != (shared.EntryRef{EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp}) {
		t.Fatalf("asked %+v", leads.asked)
	}
}

func TestCreateRefusesALeadThatIsNotTheConversationsLead(t *testing.T) {
	repo := newFakeOppRepo()
	leads := &entryLeads{byEntry: map[string]string{"ws1/entry-1": "lead-7"}}
	in := baseCreate()
	in.LeadID = "lead-1"
	in.LinkEntryID, in.LinkEntryType = "entry-1", "whatsapp"
	if _, err := serviceWithEntryLeads(repo, leads).Create("ws1", in); !errors.Is(err, opportunity.ErrEntryLeadMismatch) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if len(repo.store) != 0 || len(repo.links) != 0 {
		t.Fatal("a deal was stored on another lead than its conversation's")
	}
}

func TestCreateAcceptsTheConversationsOwnLead(t *testing.T) {
	repo := newFakeOppRepo()
	leads := &entryLeads{byEntry: map[string]string{"ws1/entry-1": "lead-7"}}
	in := baseCreate()
	in.LeadID = "lead-7"
	in.LinkEntryID, in.LinkEntryType = "entry-1", "whatsapp"
	o, err := serviceWithEntryLeads(repo, leads).Create("ws1", in)
	if err != nil {
		t.Fatal(err)
	}
	if o.LeadID != "lead-7" || len(leads.asked) != 1 {
		t.Fatalf("lead = %q, asked %+v", o.LeadID, leads.asked)
	}
}

func TestCreateKeepsTheLeadGivenForAConversationWithoutALead(t *testing.T) {
	repo := newFakeOppRepo()
	in := baseCreate()
	in.LeadID = "lead-1"
	in.LinkEntryID, in.LinkEntryType = "visitor-chat", "webchat"
	o, err := serviceWithEntryLeads(repo, &entryLeads{}).Create("ws1", in)
	if err != nil {
		t.Fatal(err)
	}
	if o.LeadID != "lead-1" {
		t.Fatalf("lead = %q, want the one given", o.LeadID)
	}
}

func TestCreateFromAConversationWithoutALeadLeavesTheLeadEmpty(t *testing.T) {
	repo := newFakeOppRepo()
	in := baseCreate()
	in.LinkEntryID, in.LinkEntryType = "visitor-chat", "webchat"
	o, err := serviceWithEntryLeads(repo, &entryLeads{}).Create("ws1", in)
	if err != nil {
		t.Fatal(err)
	}
	if o.LeadID != "" || len(repo.links) != 1 {
		t.Fatalf("lead = %q, links %+v", o.LeadID, repo.links)
	}
}

func TestCreateRefusesWhenTheConversationsLeadCannotBeRead(t *testing.T) {
	repo := newFakeOppRepo()
	in := baseCreate()
	in.LinkEntryID, in.LinkEntryType = "entry-1", "whatsapp"
	if _, err := serviceWithEntryLeads(repo, &entryLeads{err: errFakeBoom}).Create("ws1", in); !errors.Is(err, errFakeBoom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := serviceWithEntryLeads(repo, nil).Create("ws1", in); !errors.Is(err, ErrEntryLeadsMissing) {
		t.Fatalf("without the port err = %v", err)
	}
	if len(repo.store) != 0 || len(repo.links) != 0 {
		t.Fatal("a deal was stored without knowing its lead")
	}
}

func TestANewAtendimentoForALeadNeedsNoConversation(t *testing.T) {
	repo := newFakeOppRepo()
	leads := &entryLeads{}
	in := baseCreate()
	in.Title = ""
	in.LeadID = "lead-1"
	o, err := serviceWithEntryLeads(repo, leads).Create("ws1", in)
	if err != nil {
		t.Fatal(err)
	}
	if o.LeadID != "lead-1" || len(repo.links) != 0 || len(leads.asked) != 0 {
		t.Fatalf("deal %+v, links %+v, asked %+v", o, repo.links, leads.asked)
	}
}
