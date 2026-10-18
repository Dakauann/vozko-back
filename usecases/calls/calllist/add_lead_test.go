package calllist_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/calls/calllist"
	"vozko/domain/lead"
)

func seededActiveList(h *harness, l *calllist.List) {
	h.store.lists[l.ID] = l
}

func TestAddLeadPutsOneCallableLeadAtTheEndOfTheListOnce(t *testing.T) {
	h := newHarness()
	seededActiveList(h, activeList("list-1", worker))
	h.leads.leads["lead-a"] = callable("lead-a", "5511987650001")
	svc := h.service()
	add := calllist.LeadAddition{WorkspaceID: ws, ListID: "list-1", LeadID: "lead-a"}

	added, err := svc.AddLead(context.Background(), add)
	if err != nil || !added {
		t.Fatalf("first add = %v, %v", added, err)
	}
	again, err := svc.AddLead(context.Background(), add)
	if err != nil || again {
		t.Fatalf("adding the same lead again = %v, %v, want no second item", again, err)
	}
	l, _ := h.store.Get(context.Background(), ws, "list-1")
	if l.ItemCount != 1 {
		t.Fatalf("item count = %d", l.ItemCount)
	}
	if len(h.store.additions) != 2 || h.store.additions[0].Items[0].Phone != "5511987650001" || !h.store.additions[0].At.Equal(now) {
		t.Fatalf("additions = %+v", h.store.additions)
	}
}

func TestAddLeadRefusesALeadTheListRuleWouldSkip(t *testing.T) {
	optedOut := now.Add(-time.Hour)
	blocked := callable("lead-b", "5511987650002")
	blocked.Blocked = true
	out := callable("lead-c", "5511987650003")
	out.OptedOutAt = &optedOut
	cases := []struct {
		name   string
		lead   *lead.Lead
		leadID string
		want   calllist.SkipReason
	}{
		{"blocked", blocked, "lead-b", calllist.SkipReason(lead.DialRefusedBlocked)},
		{"opted out", out, "lead-c", calllist.SkipReason(lead.DialRefusedOptedOut)},
		{"no number", &lead.Lead{ID: "lead-d", WorkspaceID: ws}, "lead-d", calllist.SkipNoNumber},
		{"gone", nil, "lead-e", calllist.SkipGone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			seededActiveList(h, activeList("list-1", worker))
			if tc.lead != nil {
				h.leads.leads[tc.leadID] = tc.lead
			}
			added, err := h.service().AddLead(context.Background(), calllist.LeadAddition{WorkspaceID: ws, ListID: "list-1", LeadID: tc.leadID})
			var refusal *calllist.AdmissionRefusal
			if added || !errors.As(err, &refusal) || refusal.Reason != tc.want {
				t.Fatalf("add = %v, %v, want refusal %s", added, err, tc.want)
			}
			if len(h.store.additions) != 0 {
				t.Fatal("a refused lead must not reach the list")
			}
		})
	}
}

func TestAddLeadChecksTheHoldersOfAContactPhone(t *testing.T) {
	h := newHarness()
	l := activeList("list-1", worker)
	l.Phone = calllist.PhoneChoice{Source: calllist.PhoneContact, Label: lead.PhoneLandline}
	seededActiveList(h, l)
	owner := callable("lead-owner", "551133334444")
	owner.Blocked = true
	h.leads.identities = []*lead.Lead{owner}
	shared := callable("lead-a", "5511987650001")
	shared.Phones = []lead.ContactPhone{{Number: "551133334444", Label: lead.PhoneLandline}}
	h.leads.leads["lead-a"] = shared

	_, err := h.service().AddLead(context.Background(), calllist.LeadAddition{WorkspaceID: ws, ListID: "list-1", LeadID: "lead-a"})
	if !errors.Is(err, calllist.ErrLeadNotAdmitted) {
		t.Fatalf("add = %v, want the blocked holder to refuse the shared number", err)
	}
}

func TestAddLeadRefusesListsThatTakeNoNewLeads(t *testing.T) {
	cases := []struct {
		name   string
		list   func() *calllist.List
		listID string
		want   error
	}{
		{"unknown list", func() *calllist.List { return activeList("list-1", worker) }, "list-other", calllist.ErrListNotFound},
		{"building", func() *calllist.List { l := activeList("list-1", worker); l.Status = calllist.StatusBuilding; return l }, "list-1", calllist.ErrListBuilding},
		{"archived", func() *calllist.List { l := activeList("list-1", worker); l.Status = calllist.StatusArchived; return l }, "list-1", calllist.ErrListNotActive},
		{"full", func() *calllist.List { l := activeList("list-1", worker); l.ItemCount = calllist.MaxItems; return l }, "list-1", calllist.ErrListFull},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness()
			seededActiveList(h, tc.list())
			h.leads.leads["lead-a"] = callable("lead-a", "5511987650001")
			if _, err := h.service().AddLead(context.Background(), calllist.LeadAddition{WorkspaceID: ws, ListID: tc.listID, LeadID: "lead-a"}); !errors.Is(err, tc.want) {
				t.Fatalf("add = %v, want %v", err, tc.want)
			}
			if len(h.store.additions) != 0 {
				t.Fatal("nothing may be added")
			}
		})
	}
}

func TestAddLeadNeedsTheWorkspaceTheListAndTheLead(t *testing.T) {
	h := newHarness()
	seededActiveList(h, activeList("list-1", worker))
	svc := h.service()
	for _, add := range []calllist.LeadAddition{
		{ListID: "list-1", LeadID: "lead-a"},
		{WorkspaceID: ws, LeadID: "lead-a"},
		{WorkspaceID: ws, ListID: "list-1"},
	} {
		if _, err := svc.AddLead(context.Background(), add); err == nil {
			t.Fatalf("add %+v must be refused", add)
		}
	}
}
