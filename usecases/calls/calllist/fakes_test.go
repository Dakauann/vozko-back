package calllist_usecase

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"vozko/domain/calls/calllist"
	"vozko/domain/calls/cdr"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/sip_trunk"
	"vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

const (
	ws        = "ws-1"
	manager   = "manager-1"
	worker    = "worker-1"
	colleague = "worker-2"
	stranger  = "stranger-1"
)

var now = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

type grants map[string]map[string]bool

func (g grants) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	return g[userID][resource+":"+action]
}

func workerGrants() map[string]bool {
	return map[string]bool{"call_lists:read": true, "sip_trunks:read": true, "sip_trunks:call": true, "call_session:use": true}
}

func managerGrants() map[string]bool {
	g := workerGrants()
	g["call_lists:manage"], g["leads:read"] = true, true
	return g
}

func defaultGrants() grants {
	return grants{manager: managerGrants(), worker: workerGrants(), colleague: workerGrants(), stranger: {"call_lists:read": true}}
}

type memoryStore struct {
	mu          sync.Mutex
	lists       map[string]*calllist.List
	items       map[string]*calllist.Item
	appended    []calllist.BuildBatch
	finished    []calllist.Status
	failures    []string
	claimErr    error
	appendErr   error
	nextIDs     int
	calls       callBook
	updates     int
	itemQueries []calllist.ItemQuery
}

func newMemoryStore() *memoryStore {
	return &memoryStore{lists: map[string]*calllist.List{}, items: map[string]*calllist.Item{}}
}

func (s *memoryStore) Create(_ context.Context, l *calllist.List) (*calllist.List, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.lists[l.ID]; ok {
		copied := *existing
		return &copied, true, nil
	}
	copied := *l
	s.lists[l.ID] = &copied
	out := copied
	return &out, false, nil
}

func (s *memoryStore) Get(_ context.Context, workspaceID, id string) (*calllist.List, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.lists[id]
	if !ok || l.WorkspaceID != workspaceID {
		return nil, calllist.ErrListNotFound
	}
	copied := *l
	return &copied, nil
}

func (s *memoryStore) Page(_ context.Context, q calllist.ListQuery) (calllist.ListPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page := calllist.ListPage{Lists: []*calllist.List{}}
	for _, l := range s.lists {
		if l.WorkspaceID != q.WorkspaceID || (!q.Manages && !l.AssignedTo(q.ViewerID)) {
			continue
		}
		copied := *l
		page.Lists = append(page.Lists, &copied)
	}
	page.Total = int64(len(page.Lists))
	return page, nil
}

func (s *memoryStore) Update(_ context.Context, l *calllist.List) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.lists[l.ID]; !ok {
		return calllist.ErrListNotFound
	}
	s.updates++
	copied := *l
	s.lists[l.ID] = &copied
	return nil
}

func (s *memoryStore) Delete(_ context.Context, workspaceID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.lists[id]
	if !ok || l.WorkspaceID != workspaceID {
		return calllist.ErrListNotFound
	}
	delete(s.lists, id)
	return nil
}

func (s *memoryStore) ClaimBuild(_ context.Context, workspaceID, id, token string, at time.Time) (*calllist.List, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	l, ok := s.lists[id]
	if !ok || l.WorkspaceID != workspaceID || !l.BuildClaimable(at) {
		return nil, calllist.ErrBuildClaimLost
	}
	l.Build.Claim, l.Build.HeartbeatAt = token, &at
	l.Build.Attempts++
	copied := *l
	return &copied, nil
}

func (s *memoryStore) AppendItems(_ context.Context, b calllist.BuildBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appendErr != nil {
		return s.appendErr
	}
	l := s.lists[b.ListID]
	if l == nil || l.Build.Claim != b.Claim {
		return calllist.ErrBuildClaimLost
	}
	s.appended = append(s.appended, b)
	for _, item := range b.Items {
		s.nextIDs++
		l.ItemCount++
		stored := calllist.Item{ID: "item-" + string(rune('a'+s.nextIDs-1)), ListID: b.ListID, WorkspaceID: b.WorkspaceID, LeadID: item.LeadID,
			Phone: item.Phone, Position: l.ItemCount, State: calllist.StatePending, CreatedAt: b.At}
		s.items[stored.ID] = &stored
	}
	if l.Skipped == nil {
		l.Skipped = calllist.Skips{}
	}
	l.Skipped.Merge(b.Skipped)
	l.BuildCursor = b.Cursor
	return nil
}

func (s *memoryStore) FinishBuild(_ context.Context, _, id, token string, status calllist.Status, failure string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.lists[id]
	if l == nil || l.Build.Claim != token {
		return calllist.ErrBuildClaimLost
	}
	l.Status, l.FailureCode, l.Build.Claim = status, failure, ""
	s.finished = append(s.finished, status)
	s.failures = append(s.failures, failure)
	return nil
}

func (s *memoryStore) Buildable(_ context.Context, at time.Time, limit int) ([]calllist.BuildRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	refs := []calllist.BuildRef{}
	for _, l := range s.lists {
		if l.BuildClaimable(at) && len(refs) < limit {
			refs = append(refs, calllist.BuildRef{WorkspaceID: l.WorkspaceID, ID: l.ID})
		}
	}
	return refs, nil
}

func (s *memoryStore) FailExhausted(context.Context, time.Time) (int64, error) { return 0, nil }

func (s *memoryStore) Items(_ context.Context, q calllist.ItemQuery) (calllist.ItemPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.itemQueries = append(s.itemQueries, q)
	if err := q.Check(); err != nil {
		return calllist.ItemPage{}, err
	}
	page := calllist.ItemPage{Items: []calllist.ItemView{}}
	if q.Agenda() {
		asOf := q.AsOf
		page.AsOf = &asOf
	}
	for _, item := range s.sorted() {
		if item.ListID == q.ListID && (q.State == "" || item.State == q.State) {
			view := calllist.ItemView{Item: *item, Attempts: 1, StampedCall: calllist.FactsOf(s.calls[item.LastCallID])}
			if item.LastCallID != "" {
				view.LastCall = &cdr.Call{ID: item.LastCallID, Status: cdr.StatusCompleted, Direction: cdr.DirectionOutbound, AnsweredAt: &now}
			}
			page.Items = append(page.Items, view)
		}
	}
	return page, nil
}

func (s *memoryStore) sorted() []*calllist.Item {
	out := make([]*calllist.Item, 0, len(s.items))
	for _, item := range s.items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

func (s *memoryStore) Item(_ context.Context, workspaceID, id string) (*calllist.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok || item.WorkspaceID != workspaceID {
		return nil, calllist.ErrItemNotFound
	}
	copied := *item
	return &copied, nil
}

func (s *memoryStore) Next(_ context.Context, c calllist.NextClaim) (*calllist.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.sorted() {
		if item.State == calllist.StateReserved && item.ReservedBy == c.UserID && !item.LiveFor(c.UserID, c.Now) {
			item.State, item.ReservedBy, item.ReservedUntil = calllist.StatePending, "", nil
		}
	}
	for _, item := range s.sorted() {
		if item.LiveFor(c.UserID, c.Now) {
			if item.ListID != c.ListID {
				return nil, calllist.ErrReservationHeld
			}
			_ = item.Reserve(c.UserID, c.Now)
			copied := *item
			return &copied, nil
		}
	}
	for _, item := range s.sorted() {
		if item.ListID == c.ListID && item.Claimable(c.Now) {
			_ = item.Reserve(c.UserID, c.Now)
			copied := *item
			return &copied, nil
		}
	}
	return nil, nil
}

func (s *memoryStore) Mutate(_ context.Context, workspaceID, itemID string, fn calllist.Mutation) (*calllist.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[itemID]
	if !ok || item.WorkspaceID != workspaceID {
		return nil, calllist.ErrItemNotFound
	}
	l := s.lists[item.ListID]
	working := *item
	listCopy := *l
	if err := fn(&working, &listCopy); err != nil {
		return nil, err
	}
	change := calllist.ProgressChange(*item, working)
	*item = working
	l.ClosedCount += change.Closed
	l.CalledCount += change.Called
	l.CallbackCount += change.Callbacks
	copied := working
	return &copied, nil
}

func (s *memoryStore) put(items ...calllist.Item) {
	for i := range items {
		item := items[i]
		s.items[item.ID] = &item
	}
}

type leadBook struct {
	leads       map[string]*lead.Lead
	identities  []*lead.Lead
	loads       int
	manyLoads   [][]string
	identityAsk [][]string
}

func (b *leadBook) Load(_ context.Context, workspaceID, id string) (*lead.Lead, error) {
	b.loads++
	l, ok := b.leads[id]
	if !ok || l.WorkspaceID != workspaceID {
		return nil, lead.ErrLeadNotFound
	}
	return l, nil
}

func (b *leadBook) LoadManyForDial(_ context.Context, workspaceID string, ids []string) ([]*lead.Lead, error) {
	b.manyLoads = append(b.manyLoads, ids)
	out := []*lead.Lead{}
	for _, id := range ids {
		if l, ok := b.leads[id]; ok && l.WorkspaceID == workspaceID {
			out = append(out, l)
		}
	}
	return out, nil
}

func (b *leadBook) FindIdentities(_ context.Context, _ string, numbers []string) ([]*lead.Lead, error) {
	b.identityAsk = append(b.identityAsk, numbers)
	return b.identities, nil
}

type snapshotBook struct {
	ids     map[string][]string
	dropped []string
	pages   int
}

func (s *snapshotBook) SnapshotPage(_ context.Context, _, snapshotID, after string, limit int) ([]string, error) {
	s.pages++
	all := append([]string{}, s.ids[snapshotID]...)
	sort.Strings(all)
	out := []string{}
	for _, id := range all {
		if id > after && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *snapshotBook) DropSnapshot(_ context.Context, _, snapshotID string) error {
	s.dropped = append(s.dropped, snapshotID)
	return nil
}

type callBook map[string]*cdr.Call

func (c callBook) GetByID(id string) (*cdr.Call, error) {
	call, ok := c[id]
	if !ok {
		return nil, cdr.ErrCallNotFound
	}
	return call, nil
}

type outcomeBook struct {
	capture *conversation.OutcomeCapture
	err     error
}

func (o outcomeBook) OutcomeCaptureFor(context.Context, string) (*conversation.OutcomeCapture, error) {
	return o.capture, o.err
}

type interactionBook map[string]*calllist.LastInteraction

func (i interactionBook) Latest(_ context.Context, _, leadID string) (*calllist.LastInteraction, error) {
	return i[leadID], nil
}

type entryAccess map[string]bool

func (a entryAccess) CanAccessEntry(userID, _, entryID, _ string, _ bool) bool {
	return a[userID+":"+entryID]
}

type lineBook struct {
	plan  *lead_usecase.LeadDialPlan
	err   error
	asked []string
}

func (l *lineBook) NumberLines(_ context.Context, _ Actor, number string) (*lead_usecase.LeadDialPlan, error) {
	l.asked = append(l.asked, number)
	if l.err != nil {
		return nil, l.err
	}
	return l.plan, nil
}

type harness struct {
	store        *memoryStore
	leads        *leadBook
	snapshots    *snapshotBook
	calls        callBook
	outcomes     outcomeBook
	interactions interactionBook
	access       entryAccess
	lines        *lineBook
	grants       grants
	counted      *countedGrants
	launched     int
	clock        time.Time
}

type countedGrants struct {
	grants grants
	calls  map[string]int
}

func (c *countedGrants) HasWorkspacePermission(userID, workspaceID, resource, action string, isAdmin bool) bool {
	c.calls[userID]++
	return c.grants.HasWorkspacePermission(userID, workspaceID, resource, action, isAdmin)
}

func workRequirements(t *testing.T) []workspace.PermissionEntry {
	t.Helper()
	entries, err := workspace.CapabilitiesRequire([]workspace.CapabilityKey{calllist.CapabilityWork})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func newHarness() *harness {
	h := &harness{
		store: newMemoryStore(), leads: &leadBook{leads: map[string]*lead.Lead{}}, snapshots: &snapshotBook{ids: map[string][]string{}},
		calls: callBook{}, outcomes: outcomeBook{capture: &conversation.OutcomeCapture{Outcomes: []conversation.Outcome{
			{Code: "interessado", Label: "Interessado", IsDurable: true}, {Code: "sem_interesse", Label: "Sem interesse"},
		}}},
		interactions: interactionBook{}, access: entryAccess{},
		lines:  &lineBook{plan: &lead_usecase.LeadDialPlan{Trunks: []sip_trunk.TrunkChoice{{ID: "trunk-1", Name: "Matriz"}}}},
		grants: defaultGrants(), clock: now,
	}
	h.counted = &countedGrants{grants: h.grants, calls: map[string]int{}}
	h.store.calls = h.calls
	return h
}

func (h *harness) service() *Service {
	svc, err := NewService(Deps{
		Store: h.store, Leads: h.leads, Snapshots: h.snapshots, Calls: h.calls, Outcomes: h.outcomes,
		Interactions: h.interactions, Access: h.access, Lines: h.lines, Permissions: h.counted,
		Now: func() time.Time { return h.clock }, NewToken: func() string { return "token-1" },
		Background: func(run func()) { h.launched++; run() },
	})
	if err != nil {
		panic(err)
	}
	return svc
}

func actor(user string) Actor {
	return Actor{UserID: user, WorkspaceID: ws}
}

func activeList(id string, assignees ...string) *calllist.List {
	return &calllist.List{ID: id, WorkspaceID: ws, Name: "Retorno", CreatedBy: manager, AssigneeIDs: assignees, Status: calllist.StatusActive,
		Phone: calllist.PhoneChoice{Source: calllist.PhoneIdentity}, Skipped: calllist.Skips{}}
}

func callable(id, number string) *lead.Lead {
	return &lead.Lead{ID: id, WorkspaceID: ws, Number: number, Name: "Pessoa " + id, RelativesCount: 2}
}

var errBoom = errors.New("boom")
