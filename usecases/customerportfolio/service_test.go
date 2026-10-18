package customerportfolio_usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"vozko/domain/conversation"
	cp "vozko/domain/customerportfolio"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

var ctx = context.Background()

const ws = "ws-1"

type memoryEntries struct {
	rows    map[string]cp.Entry
	inserts int
	listed  cp.ListQuery
}

func (m *memoryEntries) Insert(_ context.Context, e cp.Entry) (cp.Entry, bool, error) {
	if existing, _ := m.FindBySubject(ctx, e.WorkspaceID, e.Subject); existing != nil {
		return *existing, false, nil
	}
	m.inserts++
	m.rows[e.ID] = e
	return e, true, nil
}

func (m *memoryEntries) Get(_ context.Context, workspaceID, id string) (cp.Entry, error) {
	e, ok := m.rows[id]
	if !ok || e.WorkspaceID != workspaceID {
		return cp.Entry{}, cp.ErrEntryNotFound
	}
	return e, nil
}

func (m *memoryEntries) FindBySubject(_ context.Context, workspaceID string, s cp.Subject) (*cp.Entry, error) {
	for _, e := range m.rows {
		if e.WorkspaceID == workspaceID && e.Subject == s {
			found := e
			return &found, nil
		}
	}
	return nil, nil
}

func (m *memoryEntries) SaveHolder(_ context.Context, e cp.Entry) error {
	if _, ok := m.rows[e.ID]; !ok {
		return cp.ErrEntryNotFound
	}
	m.rows[e.ID] = e
	return nil
}

func (m *memoryEntries) Delete(_ context.Context, workspaceID, id string) error {
	if _, err := m.Get(ctx, workspaceID, id); err != nil {
		return err
	}
	delete(m.rows, id)
	return nil
}

func (m *memoryEntries) List(_ context.Context, workspaceID string, q cp.ListQuery) (*shared.PaginatedResult[cp.Row], error) {
	m.listed = q
	var items []cp.Row
	for _, e := range m.rows {
		if e.WorkspaceID == workspaceID && (q.HolderID == "" || e.HolderID == q.HolderID) {
			items = append(items, cp.Row{Entry: e})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return &shared.PaginatedResult[cp.Row]{Items: items, TotalItems: int64(len(items))}, nil
}

type stubContacts map[string]cp.Subject

func (s stubContacts) ContactOfConversation(_ context.Context, _ string, ref shared.EntryRef) (cp.Subject, error) {
	if subject, ok := s[ref.EntryID]; ok {
		return subject, nil
	}
	return cp.Subject{}, cp.ErrSubjectNotFound
}

type stubEntryLeads struct {
	leads map[string]string
	err   error
}

func (s stubEntryLeads) LeadOfEntry(_ context.Context, _ string, ref shared.EntryRef) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if id, ok := s.leads[ref.EntryID]; ok {
		return id, nil
	}
	return "", lead.ErrLeadNotFound
}

type team struct {
	grants   map[string]map[string]bool
	members  map[string]bool
	reach    map[string]bool
	access   map[string]bool
	names    map[string]string
	reachErr error
}

func (t *team) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	return t.grants[userID][resource+":"+action]
}

func (t *team) GetDepartmentScope(userID, _ string, _ bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, t.members[userID]
}

func (t *team) CanView(caller, target, _ string, _ bool) (bool, error) {
	return t.reach[caller+">"+target], t.reachErr
}

func (t *team) CanAccessEntry(userID, _, entryID, _ string, _ bool) bool {
	return t.access[userID+">"+entryID]
}

func (t *team) ResolveNames(ids ...string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := t.names[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}

var (
	viewer   = []string{"customer_portfolios:read"}
	operator = []string{"customer_portfolios:read", "customer_portfolios:use", "conversations:read", "leads:read"}
	manager  = append(append([]string{}, operator...), "customer_portfolios:manage", "members:read")
)

func (t *team) member(id, name string, permissions ...string) {
	t.members[id], t.names[id] = true, name
	t.grants[id] = map[string]bool{}
	for _, p := range permissions {
		t.grants[id][p] = true
	}
}

type fixture struct {
	svc     *Service
	entries *memoryEntries
	team    *team
	leads   *stubEntryLeads
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	tm := &team{grants: map[string]map[string]bool{}, members: map[string]bool{}, reach: map[string]bool{}, access: map[string]bool{}, names: map[string]string{}}
	tm.member("ana", "Ana", operator...)
	tm.member("bob", "Bob", operator...)
	tm.member("mia", "Mia", manager...)
	tm.member("vic", "Vic", viewer...)
	tm.member("leo", "Leo", "conversations:read")
	tm.access["ana>conv-ig"], tm.access["ana>conv-wa"], tm.access["bob>conv-ig"], tm.access["mia>conv-ig"] = true, true, true, true
	tm.reach["mia>bob"], tm.reach["mia>ana"], tm.reach["mia>leo"] = true, true, true

	entries := &memoryEntries{rows: map[string]cp.Entry{}}
	leads := &stubEntryLeads{leads: map[string]string{"conv-wa": "lead-maria"}}
	ids := 0
	svc, err := New(Deps{
		Entries:     entries,
		Contacts:    stubContacts{"conv-ig": cp.ContactSubject(shared.EntryTypeInstagram, "ig-joana")},
		EntryLeads:  leads,
		Permissions: tm,
		Receivers:   tm,
		Access:      tm,
		Reach:       tm,
		Names:       tm,
		Now:         func() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC) },
		NewID:       func() string { ids++; return fmt.Sprintf("entry-%d", ids) },
	})
	if err != nil {
		t.Fatalf("New = %v", err)
	}
	return &fixture{svc: svc, entries: entries, team: tm, leads: leads}
}

func as(userID string) Actor { return Actor{UserID: userID, WorkspaceID: ws} }

func instagram() shared.EntryRef {
	return shared.EntryRef{EntryID: "conv-ig", EntryType: shared.EntryTypeInstagram}
}

func whatsapp() shared.EntryRef {
	return shared.EntryRef{EntryID: "conv-wa", EntryType: shared.EntryTypeWhatsApp}
}

func TestNewRefusesMissingDependencies(t *testing.T) {
	if _, err := New(Deps{}); err == nil {
		t.Fatal("New(Deps{}) must refuse to build a service that cannot check permissions")
	}
}

func TestAnOperatorPutsAnInstagramCustomerInTheirCarteiraByTheirProfile(t *testing.T) {
	f := newFixture(t)

	got, err := f.svc.PlaceConversation(ctx, as("ana"), instagram(), "")

	if err != nil {
		t.Fatalf("place = %v", err)
	}
	if got.Subject != cp.ContactSubject(shared.EntryTypeInstagram, "ig-joana") || got.Entry == nil || got.Entry.HolderID != "ana" || got.HolderName != "Ana" {
		t.Fatalf("holding = %+v", got)
	}
}

func TestAConversationWithALeadPutsTheLeadSoEveryChannelFollows(t *testing.T) {
	f := newFixture(t)

	got, err := f.svc.PlaceConversation(ctx, as("ana"), whatsapp(), "")

	if err != nil || got.Subject != cp.LeadSubject("lead-maria") {
		t.Fatalf("holding = %+v %v", got, err)
	}
}

func TestAnUnreadableLeadNeverFallsBackToTheContact(t *testing.T) {
	f := newFixture(t)
	f.leads.err = errors.New("db down")

	if _, err := f.svc.PlaceConversation(ctx, as("ana"), instagram(), ""); err == nil || errors.Is(err, cp.ErrSubjectNotFound) {
		t.Fatalf("place = %v, want the read error", err)
	}
	if len(f.entries.rows) != 0 {
		t.Fatal("nothing may be written when the lead cannot be read")
	}
}

func TestPlacingFromAConversationNeedsAccessAndThePermission(t *testing.T) {
	f := newFixture(t)
	cases := map[string]struct {
		actor Actor
		ref   shared.EntryRef
	}{
		"no access to the conversation":     {as("ana"), shared.EntryRef{EntryID: "conv-other", EntryType: shared.EntryTypeInstagram}},
		"no carteira permission":            {as("leo"), instagram()},
		"can see carteiras but not use one": {as("vic"), instagram()},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := f.svc.PlaceConversation(ctx, c.actor, c.ref, ""); !errors.Is(err, cp.ErrForbidden) {
				t.Fatalf("place = %v, want ErrForbidden", err)
			}
		})
	}
}

func TestACustomerInAColleaguesCarteiraIsRefusedWithTheHoldersName(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.PlaceConversation(ctx, as("bob"), instagram(), ""); err != nil {
		t.Fatal(err)
	}

	_, err := f.svc.PlaceConversation(ctx, as("ana"), instagram(), "")

	var held *cp.HeldByOther
	if !errors.As(err, &held) || held.Entry.HolderID != "bob" || held.HolderName != "Bob" {
		t.Fatalf("place = %#v, want HeldByOther naming Bob", err)
	}
}

func TestPuttingTheSameCustomerTwiceIsANoOp(t *testing.T) {
	f := newFixture(t)
	first, _ := f.svc.PlaceConversation(ctx, as("ana"), instagram(), "")

	again, err := f.svc.PlaceConversation(ctx, as("ana"), instagram(), "ana")

	if err != nil || again.Entry.ID != first.Entry.ID || f.entries.inserts != 1 {
		t.Fatalf("again = %+v %v, inserts=%d", again, err, f.entries.inserts)
	}
}

func TestAManagerPutsACustomerInAColleaguesCarteira(t *testing.T) {
	f := newFixture(t)

	got, err := f.svc.PlaceConversation(ctx, as("mia"), instagram(), "bob")

	if err != nil || got.Entry.HolderID != "bob" || got.Entry.AddedBy != "mia" {
		t.Fatalf("holding = %+v %v", got, err)
	}
}

func TestTheNewHolderMustBeReachableAndAbleToHoldACarteira(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.PlaceConversation(ctx, as("mia"), instagram(), "vic"); !errors.Is(err, cp.ErrHolderOutOfReach) {
		t.Fatalf("someone the manager cannot see = %v, want ErrHolderOutOfReach", err)
	}
	if _, err := f.svc.PlaceConversation(ctx, as("mia"), instagram(), "leo"); !errors.Is(err, cp.ErrHolderIneligible) {
		t.Fatalf("someone without the carteira permission = %v, want ErrHolderIneligible", err)
	}
	f.team.reachErr = errors.New("db down")
	if _, err := f.svc.PlaceConversation(ctx, as("mia"), instagram(), "bob"); err == nil {
		t.Fatal("an unreadable reach must refuse")
	}
}

func TestAnOperatorCannotFillAColleaguesCarteira(t *testing.T) {
	f := newFixture(t)

	if _, err := f.svc.PlaceConversation(ctx, as("ana"), instagram(), "bob"); !errors.Is(err, cp.ErrForbidden) {
		t.Fatalf("place = %v, want ErrForbidden", err)
	}
}

func TestPlacingALeadNeedsToReadLeads(t *testing.T) {
	f := newFixture(t)
	f.team.grants["ana"]["leads:read"] = false

	if _, err := f.svc.PlaceLead(ctx, as("ana"), "lead-maria", ""); !errors.Is(err, cp.ErrForbidden) {
		t.Fatalf("place lead = %v, want ErrForbidden", err)
	}
	got, err := f.svc.PlaceLead(ctx, as("bob"), "lead-maria", "")
	if err != nil || got.Subject != cp.LeadSubject("lead-maria") || got.Entry.HolderID != "bob" {
		t.Fatalf("place lead = %+v %v", got, err)
	}
}

func TestOnlyManagersMoveAndTheTargetMustBeEligible(t *testing.T) {
	f := newFixture(t)
	placed, _ := f.svc.PlaceConversation(ctx, as("ana"), instagram(), "")

	if _, err := f.svc.Move(ctx, as("ana"), placed.Entry.ID, "bob"); !errors.Is(err, cp.ErrForbidden) {
		t.Fatalf("operator move = %v, want ErrForbidden", err)
	}
	if _, err := f.svc.Move(ctx, as("mia"), placed.Entry.ID, "leo"); !errors.Is(err, cp.ErrHolderIneligible) {
		t.Fatalf("move to someone without carteira = %v", err)
	}
	moved, err := f.svc.Move(ctx, as("mia"), placed.Entry.ID, "bob")
	if err != nil || moved.Entry.HolderID != "bob" || moved.HolderName != "Bob" || f.entries.rows[placed.Entry.ID].HolderID != "bob" {
		t.Fatalf("move = %+v %v", moved, err)
	}
}

func TestRemovingIsForTheHolderOrAManager(t *testing.T) {
	f := newFixture(t)
	placed, _ := f.svc.PlaceConversation(ctx, as("ana"), instagram(), "")

	if err := f.svc.Remove(ctx, as("bob"), placed.Entry.ID); !errors.Is(err, cp.ErrForbidden) {
		t.Fatalf("colleague remove = %v", err)
	}
	if err := f.svc.Remove(ctx, as("ana"), placed.Entry.ID); err != nil {
		t.Fatalf("holder remove = %v", err)
	}
	again, _ := f.svc.PlaceConversation(ctx, as("ana"), instagram(), "")
	if err := f.svc.Remove(ctx, as("mia"), again.Entry.ID); err != nil {
		t.Fatalf("manager remove = %v", err)
	}
	if err := f.svc.Remove(ctx, as("mia"), "missing"); !errors.Is(err, cp.ErrEntryNotFound) {
		t.Fatalf("remove missing = %v", err)
	}
}

func TestListShowsNamesAndWhetherTheHolderStillReceives(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.PlaceConversation(ctx, as("ana"), instagram(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.PlaceLead(ctx, as("bob"), "lead-maria", ""); err != nil {
		t.Fatal(err)
	}
	f.team.grants["bob"]["customer_portfolios:use"] = false

	mine, err := f.svc.List(ctx, as("ana"), cp.ListQuery{HolderID: "ana"})
	if err != nil || len(mine.Items) != 1 || mine.Items[0].HolderName != "Ana" || mine.Items[0].AddedByName != "Ana" || !mine.Items[0].HolderReceives {
		t.Fatalf("ana's list = %+v %v", mine, err)
	}
	if _, err := f.svc.List(ctx, as("ana"), cp.ListQuery{}); !errors.Is(err, cp.ErrForbidden) {
		t.Fatalf("operator listing everyone = %v", err)
	}
	everyone, err := f.svc.List(ctx, as("mia"), cp.ListQuery{})
	if err != nil || len(everyone.Items) != 2 {
		t.Fatalf("manager list = %+v %v", everyone, err)
	}
	for _, row := range everyone.Items {
		if row.HolderID == "bob" && row.HolderReceives {
			t.Fatal("a holder who lost the carteira permission must show as not receiving")
		}
	}
	if _, err := f.svc.List(ctx, as("ana"), cp.ListQuery{HolderID: "ana", Page: -1}); !errors.Is(err, cp.ErrListQueryInvalid) {
		t.Fatalf("invalid query = %v", err)
	}
}

func TestHoldingOfAConversationOrLead(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.PlaceConversation(ctx, as("bob"), instagram(), ""); err != nil {
		t.Fatal(err)
	}

	got, err := f.svc.ForConversation(ctx, as("ana"), instagram())
	if err != nil || got.Entry == nil || got.HolderName != "Bob" {
		t.Fatalf("for conversation = %+v %v", got, err)
	}
	free, err := f.svc.ForLead(ctx, as("ana"), "lead-maria")
	if err != nil || free.Entry != nil || free.Subject != cp.LeadSubject("lead-maria") {
		t.Fatalf("for free lead = %+v %v", free, err)
	}
	if _, err := f.svc.ForConversation(ctx, as("leo"), instagram()); !errors.Is(err, cp.ErrForbidden) {
		t.Fatalf("without view = %v", err)
	}
	if _, err := f.svc.ForConversation(ctx, as("ana"), shared.EntryRef{EntryID: "conv-other", EntryType: shared.EntryTypeInstagram}); !errors.Is(err, cp.ErrForbidden) {
		t.Fatalf("someone else's conversation = %v", err)
	}
}
