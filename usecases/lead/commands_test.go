package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"vozko/domain/lead"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
)

const (
	cmdWorkspace = "ws-1"
	cmdUser      = "4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00"
	otherUser    = "9f8e7d6c-5b4a-4321-8765-0fedcba98765"
	cmdAgent     = "ai:1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
)

var cmdNow = time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)

type savedCall struct {
	leadID   string
	expected int64
	events   []recordevent.Event
}

type fakeStore struct {
	leads     map[string]*lead.Lead
	relations map[string]lead.Relation
	saves     []savedCall
	inserted  []*lead.Lead
	added     []lead.Relation
	removedBy []string
	raceOnce  bool
	nextID    int
	clock     int
}

func newFakeStore(leads ...*lead.Lead) *fakeStore {
	s := &fakeStore{leads: map[string]*lead.Lead{}, relations: map[string]lead.Relation{}}
	for _, l := range leads {
		s.leads[l.ID] = l
	}
	return s
}

func (s *fakeStore) FindByID(workspaceID, id string) (*lead.Lead, error) {
	l, ok := s.leads[id]
	if !ok || l.WorkspaceID != workspaceID {
		return nil, lead.ErrLeadNotFound
	}
	copied := *l
	copied.Phones, copied.Addresses, copied.Relations = nil, nil, nil
	return &copied, nil
}

func (s *fakeStore) Load(_ context.Context, workspaceID, id string) (*lead.Lead, error) {
	l, ok := s.leads[id]
	if !ok || l.WorkspaceID != workspaceID {
		return nil, lead.ErrLeadNotFound
	}
	copied := *l
	copied.Phones = append([]lead.ContactPhone{}, l.Phones...)
	copied.Addresses = append([]lead.Address{}, l.Addresses...)
	copied.Relations = nil
	return &copied, nil
}

func (s *fakeStore) FindByNumber(workspaceID, number string) (*lead.Lead, error) {
	for _, l := range s.leads {
		if l.WorkspaceID == workspaceID && l.Number == number {
			copied := *l
			return &copied, nil
		}
	}
	return nil, lead.ErrLeadNotFound
}

func (s *fakeStore) id(prefix string) string {
	s.nextID++
	return fmt.Sprintf("%s-%d", prefix, s.nextID)
}

func (s *fakeStore) Insert(_ context.Context, l *lead.Lead, events []recordevent.Event) error {
	if l.HasIdentity() {
		if _, err := s.FindByNumber(l.WorkspaceID, l.Number); err == nil {
			return lead.ErrLeadDuplicate
		}
	}
	if l.ID == "" {
		l.ID = "l-new"
	}
	l.Version = 1
	s.assignIDs(l)
	for i := range l.Relations {
		r := &l.Relations[i]
		r.ID, r.CreatedAt = s.id("r"), s.tick()
		s.relations[r.ID] = *r
		s.shift(r.Other(l.ID), r.CountsFor(r.Other(l.ID)))
	}
	copied := *l
	s.leads[l.ID] = &copied
	s.inserted = append(s.inserted, l)
	s.saves = append(s.saves, savedCall{leadID: l.ID, events: events})
	return nil
}

func (s *fakeStore) Save(_ context.Context, l *lead.Lead, expected int64, events []recordevent.Event) error {
	stored, ok := s.leads[l.ID]
	if !ok || stored.WorkspaceID != l.WorkspaceID {
		return lead.ErrLeadNotFound
	}
	if s.raceOnce {
		s.raceOnce = false
		stored.Version++
		return shared.ErrVersionConflict
	}
	if stored.Version != expected {
		return shared.ErrVersionConflict
	}
	l.Version = expected + 1
	s.assignIDs(l)
	copied := *l
	copied.RelativesCount, copied.ReferredCount = stored.RelativesCount, stored.ReferredCount
	s.leads[l.ID] = &copied
	s.saves = append(s.saves, savedCall{leadID: l.ID, expected: expected, events: events})
	return nil
}

func (s *fakeStore) assignIDs(l *lead.Lead) {
	for i := range l.Phones {
		if l.Phones[i].ID == "" {
			l.Phones[i].ID = s.id("p")
		}
	}
	for i := range l.Addresses {
		if l.Addresses[i].ID == "" {
			l.Addresses[i].ID = s.id("a")
		}
	}
}

func (s *fakeStore) tick() time.Time {
	s.clock++
	return cmdNow.Add(time.Duration(s.clock) * time.Second)
}

func (s *fakeStore) shift(id string, delta lead.RelationCounts) {
	other, ok := s.leads[id]
	if !ok {
		return
	}
	other.RelativesCount += delta.Relatives
	other.ReferredCount += delta.Referred
	other.Version++
}

func (s *fakeStore) tallies(r lead.Relation) map[string]lead.RelationTally {
	tallies := map[string]lead.RelationTally{}
	for _, side := range []string{r.LeadID, r.OtherLeadID} {
		if l, ok := s.leads[side]; ok {
			tallies[side] = lead.RelationTally{Version: l.Version, Counts: lead.RelationCounts{Relatives: l.RelativesCount, Referred: l.ReferredCount}}
		}
	}
	return tallies
}

func (s *fakeStore) AddRelation(_ context.Context, workspaceID string, r lead.Relation) (lead.RelationWrite, error) {
	for _, side := range []string{r.LeadID, r.OtherLeadID} {
		if l, ok := s.leads[side]; !ok || l.WorkspaceID != workspaceID {
			return lead.RelationWrite{}, lead.ErrRelativeNotFound
		}
	}
	for _, existing := range s.relations {
		samePair := (existing.LeadID == r.LeadID && existing.OtherLeadID == r.OtherLeadID) || (existing.LeadID == r.OtherLeadID && existing.OtherLeadID == r.LeadID)
		if samePair && existing.Dimension() == r.Dimension() {
			return lead.RelationWrite{}, lead.ErrRelationExists
		}
	}
	r.ID, r.CreatedAt = s.id("r"), s.tick()
	s.relations[r.ID] = r
	s.added = append(s.added, r)
	s.shift(r.LeadID, r.CountsFor(r.LeadID))
	s.shift(r.OtherLeadID, r.CountsFor(r.OtherLeadID))
	return lead.RelationWrite{Relation: r, Leads: s.tallies(r)}, nil
}

func (s *fakeStore) RemoveRelation(_ context.Context, _, relationID, actorID string) (lead.RelationWrite, error) {
	r, ok := s.relations[relationID]
	if !ok {
		return lead.RelationWrite{}, lead.ErrRelationNotFound
	}
	delete(s.relations, relationID)
	s.removedBy = append(s.removedBy, actorID)
	s.shift(r.LeadID, lead.RelationCounts{}.Minus(r.CountsFor(r.LeadID)))
	s.shift(r.OtherLeadID, lead.RelationCounts{}.Minus(r.CountsFor(r.OtherLeadID)))
	return lead.RelationWrite{Relation: r, Leads: s.tallies(r)}, nil
}

func (s *fakeStore) ListRelatives(_ context.Context, workspaceID string, q lead.RelativesQuery) (lead.RelativesPage, error) {
	q, err := q.Normalize()
	if err != nil {
		return lead.RelativesPage{}, err
	}
	page := lead.RelativesPage{Relatives: []lead.Relative{}}
	for _, r := range s.relations {
		other, live := s.leads[r.Other(q.LeadID)]
		if !r.Involves(q.LeadID) || !live || other.WorkspaceID != workspaceID || (q.Dimension != "" && r.Dimension() != q.Dimension) {
			continue
		}
		page.Relatives = append(page.Relatives, lead.Relative{Relation: r, Lead: &lead.Lead{ID: other.ID, Name: other.Name, Number: other.Number}})
	}
	sort.Slice(page.Relatives, func(i, j int) bool {
		return page.Relatives[i].Relation.CreatedAt.Before(page.Relatives[j].Relation.CreatedAt)
	})
	if len(page.Relatives) > q.Limit {
		page.Relatives = page.Relatives[:q.Limit]
		last := page.Relatives[q.Limit-1].Relation
		page.Next = lead.RelativesCursor{CreatedAt: last.CreatedAt, RelationID: last.ID}.Encode()
	}
	return page, nil
}

type fakePermissions map[string]bool

func (p fakePermissions) HasWorkspacePermission(_, _, resource, action string, _ bool) bool {
	return p[resource+":"+action]
}

type fakeOwners map[string]bool

func (o fakeOwners) Belongs(_, actorID string) (bool, error) { return o[actorID], nil }

type fakeVisibility map[string]bool

func (v fakeVisibility) CanView(_, target, _ string, _ bool) (bool, error) { return v[target], nil }

type fakeNotifier struct{ changes []lead.Change }

func (n *fakeNotifier) LeadChanged(c lead.Change) { n.changes = append(n.changes, c) }

type fakePhones map[string]*businessphone.WhatsAppBusinessPhoneNumber

func (p fakePhones) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if phone, ok := p[id]; ok {
		return phone, nil
	}
	return nil, businessphone.ErrPhoneNumberNotFound
}

type fakeMeta struct {
	blocked   []string
	unblocked []string
	fail      bool
}

func (m *fakeMeta) BlockUser(_, number, _ string) error {
	if m.fail {
		return errors.New("meta down")
	}
	m.blocked = append(m.blocked, number)
	return nil
}

func (m *fakeMeta) UnblockUser(_, number, _ string) error {
	m.unblocked = append(m.unblocked, number)
	return nil
}

type fakeGrants map[string]bool

func (g fakeGrants) HasAccess(workspaceID, phoneID string) (bool, error) {
	return g[workspaceID+":"+phoneID], nil
}

type commandsFixture struct {
	store      *fakeStore
	perms      fakePermissions
	notifier   *fakeNotifier
	meta       *fakeMeta
	entries    *fakeEntries
	duplicates *fakeDuplicates
	fields     *fakeDefinitions
	anonymizer *fakeAnonymizer
	cmds       *Commands
}

func allPermissions() fakePermissions {
	return fakePermissions{"leads:read": true, "leads:create": true, "leads:update": true, "leads:block": true, "leads:assign": true}
}

func newCommandsFixture(t *testing.T, perms fakePermissions, leads ...*lead.Lead) *commandsFixture {
	t.Helper()
	f := &commandsFixture{store: newFakeStore(leads...), perms: perms, notifier: &fakeNotifier{}, meta: &fakeMeta{}, entries: &fakeEntries{}, duplicates: &fakeDuplicates{},
		fields: &fakeDefinitions{defs: leadDefinitions()}, anonymizer: &fakeAnonymizer{}}
	f.anonymizer.store = f.store
	cmds, err := NewCommands(CommandDeps{
		Store:       f.store,
		Permissions: f.perms,
		Owners:      fakeOwners{cmdUser: true, otherUser: true, cmdAgent: true},
		Visibility:  fakeVisibility{cmdUser: true},
		Notifier:    f.notifier,
		Phones: fakePhones{
			"bp-1":         {ID: "bp-1", OwnerWorkspaceID: cmdWorkspace, AccessToken: "token", MetaPhoneNumberID: "meta-1"},
			"bp-2":         {ID: "bp-2", OwnerWorkspaceID: "ws-2", AccessToken: "token", MetaPhoneNumberID: "meta-2"},
			"bp-platform":  {ID: "bp-platform", AccessToken: "token", MetaPhoneNumberID: "meta-p"},
			"bp-ungranted": {ID: "bp-ungranted", AccessToken: "token", MetaPhoneNumberID: "meta-u"},
		},
		PhoneGrants: fakeGrants{cmdWorkspace + ":bp-platform": true},
		Meta:        f.meta,
		Entries:     f.entries,
		Relations:   f.store,
		Duplicates:  f.duplicates,
		Definitions: f.fields,
		Anonymizer:  f.anonymizer,
		Now:         func() time.Time { return cmdNow },
	})
	if err != nil {
		t.Fatalf("NewCommands: %v", err)
	}
	f.cmds = cmds
	return f
}

func storedLead() *lead.Lead {
	return &lead.Lead{ID: "l-1", WorkspaceID: cmdWorkspace, Number: "5511987654321", Name: "Ana", NameSource: lead.SourceChannel, Email: "ana@x.com", CustomFields: map[string]any{"cor": "azul"}, Version: 3}
}

func operator() Actor { return Actor{UserID: cmdUser, WorkspaceID: cmdWorkspace} }

func version(v int64) *int64 { return &v }

func text(s string) *string { return &s }

func completeCommandDeps() CommandDeps {
	return CommandDeps{
		Store: newFakeStore(), Permissions: fakePermissions{}, Owners: fakeOwners{}, Visibility: fakeVisibility{}, Notifier: &fakeNotifier{},
		Phones: fakePhones{}, PhoneGrants: fakeGrants{}, Meta: &fakeMeta{},
		Entries: &fakeEntries{}, Relations: newFakeStore(), Duplicates: &fakeDuplicates{},
		Definitions: &fakeDefinitions{}, Anonymizer: &fakeAnonymizer{},
	}
}

func TestNewCommandsRefusesAMissingDependency(t *testing.T) {
	full := completeCommandDeps()
	missing := map[string]func(d *CommandDeps){
		"store":        func(d *CommandDeps) { d.Store = nil },
		"permissions":  func(d *CommandDeps) { d.Permissions = nil },
		"owners":       func(d *CommandDeps) { d.Owners = nil },
		"visibility":   func(d *CommandDeps) { d.Visibility = nil },
		"notifier":     func(d *CommandDeps) { d.Notifier = nil },
		"phones":       func(d *CommandDeps) { d.Phones = nil },
		"phone grants": func(d *CommandDeps) { d.PhoneGrants = nil },
		"meta":         func(d *CommandDeps) { d.Meta = nil },
	}
	for name, drop := range missing {
		t.Run(name, func(t *testing.T) {
			deps := full
			drop(&deps)
			if _, err := NewCommands(deps); err == nil {
				t.Fatalf("commands without %s must not be built", name)
			}
		})
	}
	if _, err := NewCommands(full); err != nil {
		t.Fatalf("complete dependencies: %v", err)
	}
}

func TestEveryCommandChecksItsPermission(t *testing.T) {
	f := newCommandsFixture(t, fakePermissions{}, storedLead())
	ctx := context.Background()
	calls := map[string]func() error{
		"create": func() error { _, err := f.cmds.Create(ctx, operator(), lead.Draft{Name: "Maria"}); return err },
		"update": func() error {
			_, err := f.cmds.Update(ctx, operator(), "l-1", version(3), lead.Edit{Nickname: text("Aninha")})
			return err
		},
		"rename":    func() error { _, err := f.cmds.Rename(ctx, operator(), "l-1", version(3), "Ana Souza"); return err },
		"block":     func() error { _, err := f.cmds.Block(ctx, operator(), "l-1", BlockInput{Blocked: true}); return err },
		"set owner": func() error { _, err := f.cmds.SetOwner(ctx, operator(), "l-1", cmdUser); return err },
		"opt out":   func() error { _, err := f.cmds.OptOut(ctx, operator(), "l-1", lead.OptOutLeadRequest); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, lead.ErrLeadForbidden) {
				t.Fatalf("err = %v, want ErrLeadForbidden", err)
			}
		})
	}
	if len(f.store.saves) != 0 || len(f.notifier.changes) != 0 {
		t.Fatal("a refused command must not write or notify")
	}
}

func TestCreateRecordsAManualLeadWithItsEventAndNotifies(t *testing.T) {
	f := newCommandsFixture(t, allPermissions())
	got, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Maria", Email: "maria@x.com"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Lead.ID != "l-new" || got.Lead.Version != 1 || got.Lead.Source != lead.SourceManual {
		t.Fatalf("created = %+v", got)
	}
	if events := f.store.saves[0].events; len(events) != 1 || events[0].Kind != lead.EventCreated || events[0].Actor != cmdUser {
		t.Fatalf("events = %+v", events)
	}
	want := lead.Change{WorkspaceID: cmdWorkspace, LeadID: "l-new", Version: 1, Fields: []string{lead.FieldEmail, lead.FieldName}}
	if len(f.notifier.changes) != 1 || !reflect.DeepEqual(f.notifier.changes[0], want) {
		t.Fatalf("changes = %+v", f.notifier.changes)
	}
}

func TestCreateRefusesATakenIdentity(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	if _, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Number: "11987654321"}); !errors.Is(err, lead.ErrLeadDuplicate) {
		t.Fatalf("err = %v, want ErrLeadDuplicate", err)
	}
}

func TestUpdateRequiresTheVersionThatWasRead(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	if _, err := f.cmds.Update(context.Background(), operator(), "l-1", nil, lead.Edit{Nickname: text("Aninha")}); !errors.Is(err, shared.ErrVersionRequired) {
		t.Fatalf("err = %v, want ErrVersionRequired", err)
	}
	if len(f.store.saves) != 0 {
		t.Fatal("nothing may be written without a version")
	}
}

func TestAStaleUpdateIsAConflictWithTheCurrentRecordProjectedForTheCaller(t *testing.T) {
	perms := fakePermissions{"leads:update": true}
	f := newCommandsFixture(t, perms, storedLead())
	_, err := f.cmds.Update(context.Background(), operator(), "l-1", version(2), lead.Edit{Nickname: text("Aninha")})
	var conflict *lead.VersionConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a version conflict", err)
	}
	if conflict.Current.Version != 3 || conflict.Current.Email != "" || conflict.Current.Number != "" {
		t.Fatalf("a caller without leads:read sees only what it sent: %+v", conflict.Current)
	}
}

func TestUpdateKeepsWhatItDidNotReceiveAndRecordsTheChange(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	got, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{Nickname: text("Aninha")})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Version != 4 || got.Nickname != "Aninha" || got.Email != "ana@x.com" || got.CustomFields["cor"] != "azul" {
		t.Fatalf("updated = %+v", got)
	}
	saved := f.store.saves[0]
	if saved.expected != 3 || len(saved.events) != 1 || saved.events[0].Kind != lead.EventUpdated {
		t.Fatalf("save = %+v", saved)
	}
	if !reflect.DeepEqual(f.notifier.changes[0].Fields, []string{lead.FieldNickname}) || f.notifier.changes[0].Version != 4 {
		t.Fatalf("change = %+v", f.notifier.changes[0])
	}
}

func TestAnUpdateThatChangesNothingWritesNothing(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	got, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{Email: text("ANA@x.com")})
	if err != nil || got.Version != 3 {
		t.Fatalf("Update = %+v, %v", got, err)
	}
	if len(f.store.saves) != 0 || len(f.notifier.changes) != 0 {
		t.Fatal("an edit without a change must not write")
	}
}

func TestRenameIsAVersionedManualEdit(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	if _, err := f.cmds.Rename(context.Background(), operator(), "l-1", nil, "Ana Souza"); !errors.Is(err, shared.ErrVersionRequired) {
		t.Fatalf("err = %v, want ErrVersionRequired", err)
	}
	got, err := f.cmds.Rename(context.Background(), operator(), "l-1", version(3), "  Ana   Souza ")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got.Name != "Ana Souza" || got.NameSource != lead.SourceManual || got.Version != 4 {
		t.Fatalf("renamed = %+v", got)
	}
	if f.store.saves[0].events[0].Kind != lead.EventRenamed {
		t.Fatalf("event = %+v", f.store.saves[0].events[0])
	}
}

func TestARenameThatLosesARaceReturnsTheNewerRecord(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	f.store.raceOnce = true
	_, err := f.cmds.Rename(context.Background(), operator(), "l-1", version(3), "Ana Souza")
	var conflict *lead.VersionConflict
	if !errors.As(err, &conflict) || conflict.Current.Version != 4 {
		t.Fatalf("err = %v, want a conflict carrying version 4", err)
	}
}

func TestUpdateOfAnotherWorkspaceLeadIsNotFound(t *testing.T) {
	foreign := storedLead()
	foreign.WorkspaceID = "ws-2"
	f := newCommandsFixture(t, allPermissions(), foreign)
	if _, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{Nickname: text("x")}); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("err = %v, want ErrLeadNotFound", err)
	}
}

func TestBlockIsTargetedIdempotentAndAppliesTheMetaBlockOnAPhoneOfTheWorkspace(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	ctx := context.Background()

	result, err := f.cmds.Block(ctx, operator(), "l-1", BlockInput{Blocked: true, BusinessPhoneID: "bp-1"})
	if err != nil {
		t.Fatalf("Block: %v", err)
	}
	if !result.Lead.Blocked || result.Lead.BlockedBy == nil || *result.Lead.BlockedBy != cmdUser || !result.MetaApplied {
		t.Fatalf("result = %+v", result)
	}
	if !reflect.DeepEqual(f.meta.blocked, []string{"5511987654321"}) || f.store.saves[0].events[0].Kind != lead.EventBlocked {
		t.Fatalf("meta = %v, events = %+v", f.meta.blocked, f.store.saves[0].events)
	}

	again, err := f.cmds.Block(ctx, operator(), "l-1", BlockInput{Blocked: true})
	if err != nil || len(f.store.saves) != 1 || again.Lead.Version != 4 {
		t.Fatalf("blocking twice must not write again: %+v, %v", again, err)
	}

	unblocked, err := f.cmds.Block(ctx, operator(), "l-1", BlockInput{Blocked: false})
	if err != nil || unblocked.Lead.Blocked || f.store.saves[1].events[0].Kind != lead.EventUnblocked {
		t.Fatalf("unblock = %+v, %v", unblocked, err)
	}
}

func TestBlockNeverUsesAPhoneOfAnotherWorkspaceAndSurvivesAMetaFailure(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	result, err := f.cmds.Block(context.Background(), operator(), "l-1", BlockInput{Blocked: true, BusinessPhoneID: "bp-2"})
	if err != nil || result.MetaApplied || len(f.meta.blocked) != 0 {
		t.Fatalf("a phone of another workspace must not be used: %+v, %v", result, err)
	}

	f2 := newCommandsFixture(t, allPermissions(), storedLead())
	f2.meta.fail = true
	result, err = f2.cmds.Block(context.Background(), operator(), "l-1", BlockInput{Blocked: true, BusinessPhoneID: "bp-1"})
	if err != nil || result.MetaApplied || !result.Lead.Blocked {
		t.Fatalf("a Meta failure keeps the local block: %+v, %v", result, err)
	}
}

func TestSetOwner(t *testing.T) {
	cases := []struct {
		name    string
		owner   string
		wantErr error
	}{
		{"a member the actor can see", cmdUser, nil},
		{"an agent of the workspace", cmdAgent, nil},
		{"nobody", "", nil},
		{"someone outside the workspace", "0f0f0f0f-0000-4000-8000-000000000000", lead.ErrLeadOwnerOutsideWorkspace},
		{"a member the actor cannot see", otherUser, lead.ErrLeadOwnerOutOfReach},
		{"the system", "system", lead.ErrLeadOwnerInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stored := storedLead()
			stored.Owner = "ai:ffffffff-ffff-4fff-8fff-ffffffffffff"
			f := newCommandsFixture(t, allPermissions(), stored)
			got, err := f.cmds.SetOwner(context.Background(), operator(), "l-1", tc.owner)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if len(f.store.saves) != 0 {
					t.Fatal("a refused owner must not be written")
				}
				return
			}
			if got.Owner != tc.owner || f.store.saves[0].events[0].Kind != lead.EventOwnerChange {
				t.Fatalf("owner = %q, events %+v", got.Owner, f.store.saves[0].events)
			}
		})
	}
}

func TestATargetedCommandRetriesAfterLosingARace(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	f.store.raceOnce = true
	got, err := f.cmds.SetOwner(context.Background(), operator(), "l-1", cmdUser)
	if err != nil || got.Owner != cmdUser || got.Version != 5 {
		t.Fatalf("SetOwner after a race = %+v, %v", got, err)
	}
}

func TestOptOutIsRecordedOnceAndNeedsLeadsUpdate(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	got, err := f.cmds.OptOut(context.Background(), operator(), "l-1", lead.OptOutLeadRequest)
	if err != nil || got.OptedOutAt == nil || !got.OptedOutAt.Equal(cmdNow) || got.OptOutSource != lead.OptOutLeadRequest {
		t.Fatalf("OptOut = %+v, %v", got, err)
	}
	event := f.store.saves[0].events[0]
	if event.Kind != lead.EventOptedOut {
		t.Fatalf("event = %+v", event)
	}
	recordedSource := false
	for _, change := range event.Changes {
		if change.Field == lead.FieldOptOutSource && change.After == "lead_request" {
			recordedSource = true
		}
	}
	if !recordedSource {
		t.Fatalf("the opt-out event does not record its source: %+v", event.Changes)
	}
	if _, err := f.cmds.OptOut(context.Background(), operator(), "l-1", lead.OptOutOperator); err != nil || len(f.store.saves) != 1 {
		t.Fatalf("opting out twice must not write again: %v", err)
	}
}

func TestOptOutRefusesAnUnknownSourceWithoutWriting(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	if _, err := f.cmds.OptOut(context.Background(), operator(), "l-1", "rumour"); !errors.Is(err, lead.ErrLeadOptOutSourceInvalid) {
		t.Fatalf("OptOut(rumour) err = %v, want the source refusal", err)
	}
	if len(f.store.saves) != 0 || len(f.notifier.changes) != 0 {
		t.Fatal("a refused opt-out must not write or notify")
	}
}

type alwaysConflicting struct{ *fakeStore }

func (s alwaysConflicting) Save(context.Context, *lead.Lead, int64, []recordevent.Event) error {
	return shared.ErrVersionConflict
}

func TestATargetedCommandThatKeepsLosingRacesIsAConflictNotAFailure(t *testing.T) {
	store := newFakeStore(storedLead())
	cmds, err := NewCommands(CommandDeps{
		Store: alwaysConflicting{store}, Permissions: allPermissions(), Owners: fakeOwners{cmdUser: true},
		Visibility: fakeVisibility{cmdUser: true}, Notifier: &fakeNotifier{}, Now: func() time.Time { return cmdNow },
		Phones: fakePhones{}, PhoneGrants: fakeGrants{}, Meta: &fakeMeta{},
		Entries: &fakeEntries{}, Relations: store, Duplicates: &fakeDuplicates{},
		Definitions: &fakeDefinitions{}, Anonymizer: &fakeAnonymizer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cmds.Block(context.Background(), operator(), "l-1", BlockInput{Blocked: true})
	var conflict *lead.VersionConflict
	if !errors.As(err, &conflict) || conflict.Current.ID != "l-1" {
		t.Fatalf("err = %v, want a version conflict with the current record", err)
	}
}

func TestBlockUsesAPlatformPhoneOnlyWhenItIsGrantedToTheWorkspace(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	result, err := f.cmds.Block(context.Background(), operator(), "l-1", BlockInput{Blocked: true, BusinessPhoneID: "bp-platform"})
	if err != nil || !result.MetaApplied || !reflect.DeepEqual(f.meta.blocked, []string{"5511987654321"}) {
		t.Fatalf("a granted platform phone must apply the Meta block: %+v, %v, %v", result, err, f.meta.blocked)
	}

	f2 := newCommandsFixture(t, allPermissions(), storedLead())
	result, err = f2.cmds.Block(context.Background(), operator(), "l-1", BlockInput{Blocked: true, BusinessPhoneID: "bp-ungranted"})
	if err != nil || result.MetaApplied || len(f2.meta.blocked) != 0 || !result.Lead.Blocked {
		t.Fatalf("a platform phone that is not granted must not be used: %+v, %v", result, err)
	}
}

func TestAnOptOutWithoutASourceIsRecordedAsTheTeamsDecision(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	got, err := f.cmds.OptOut(context.Background(), operator(), "l-1", "")
	if err != nil || got.OptOutSource != lead.OptOutOperator {
		t.Fatalf("OptOut(no source) = %+v, %v, want the operator source", got, err)
	}
}
