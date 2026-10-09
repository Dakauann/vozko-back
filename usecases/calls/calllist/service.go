package calllist_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/calllist"
	"vozko/domain/calls/cdr"
	"vozko/domain/callsession"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/sip_trunk"
	"vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

const (
	identityBuildPage = 1000
	contactBuildPage  = 200
	maxRefusedPerNext = 25
	sweepBuilds       = 10
)

var errIncomplete = errors.New("call lists: a required dependency is missing")

type Actor = conversation.Viewer

type Leads interface {
	Load(ctx context.Context, workspaceID, id string) (*lead.Lead, error)
	LoadManyForDial(ctx context.Context, workspaceID string, ids []string) ([]*lead.Lead, error)
	FindIdentities(ctx context.Context, workspaceID string, numbers []string) ([]*lead.Lead, error)
}

type Snapshots interface {
	SnapshotPage(ctx context.Context, workspaceID, snapshotID, after string, limit int) ([]string, error)
	DropSnapshot(ctx context.Context, workspaceID, snapshotID string) error
}

type Calls interface {
	GetByID(id string) (*cdr.Call, error)
}

type Outcomes interface {
	OutcomeCaptureFor(ctx context.Context, workspaceID string) (*conversation.OutcomeCapture, error)
}

type Interactions interface {
	Latest(ctx context.Context, workspaceID, leadID string) (*calllist.LastInteraction, error)
}

type Lines interface {
	NumberLines(ctx context.Context, actor Actor, number string) (*lead_usecase.LeadDialPlan, error)
}

type Deps struct {
	Store        calllist.Store
	Leads        Leads
	Snapshots    Snapshots
	Calls        Calls
	Outcomes     Outcomes
	Interactions Interactions
	Access       shared.EntryAccessChecker
	Lines        Lines
	Permissions  workspace.PermissionChecker
	Now          func() time.Time
	NewToken     func() string
	Background   func(func())
}

type Service struct {
	deps Deps
}

var _ callsession.CallListItems = (*Service)(nil)

func NewService(deps Deps) (*Service, error) {
	missing := map[string]bool{
		"store":        deps.Store == nil,
		"leads":        deps.Leads == nil,
		"snapshots":    deps.Snapshots == nil,
		"calls":        deps.Calls == nil,
		"outcomes":     deps.Outcomes == nil,
		"interactions": deps.Interactions == nil,
		"access":       deps.Access == nil,
		"lines":        deps.Lines == nil,
		"permissions":  deps.Permissions == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errIncomplete, name)
		}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.NewToken == nil {
		deps.NewToken = uuid.NewString
	}
	if deps.Background == nil {
		deps.Background = func(run func()) { go run() }
	}
	return &Service{deps: deps}, nil
}

func logf(format string, args ...any) {
	log.Printf("[call-lists] "+format, args...)
}

func (s *Service) now() time.Time {
	return s.deps.Now().UTC()
}

func (s *Service) holds(a Actor, key workspace.CapabilityKey) bool {
	return calllist.Holds(s.deps.Permissions, a.WorkspaceID, a.UserID, a.IsAdmin, key)
}

func (s *Service) require(a Actor, key workspace.CapabilityKey) error {
	if strings.TrimSpace(a.WorkspaceID) == "" {
		return calllist.ErrWorkspaceRequired
	}
	if strings.TrimSpace(a.UserID) == "" {
		return calllist.ErrActorRequired
	}
	if !s.holds(a, key) {
		return calllist.ErrForbidden
	}
	return nil
}

func (s *Service) checkAssignees(workspaceID string, ids []string) error {
	for _, id := range ids {
		if !s.holds(Actor{UserID: id, WorkspaceID: workspaceID}, calllist.CapabilityWork) {
			return fmt.Errorf("%w: %s", calllist.ErrAssigneeCannotWork, id)
		}
	}
	return nil
}

type Prepared struct {
	draft calllist.Draft
}

func (p Prepared) Draft() calllist.Draft {
	return p.draft
}

func (p Prepared) preparedFor(a Actor) bool {
	return p.draft.WorkspaceID != "" && p.draft.CreatedBy != "" && p.draft.WorkspaceID == a.WorkspaceID && p.draft.CreatedBy == a.UserID
}

func (s *Service) Prepare(ctx context.Context, a Actor, d calllist.Draft) (Prepared, error) {
	if err := s.require(a, calllist.CapabilityManage); err != nil {
		return Prepared{}, err
	}
	d.WorkspaceID, d.CreatedBy = a.WorkspaceID, a.UserID
	d = d.Normalized()
	if err := d.Validate(); err != nil {
		return Prepared{}, err
	}
	if err := s.checkAssignees(d.WorkspaceID, d.AssigneeIDs); err != nil {
		return Prepared{}, err
	}
	return Prepared{draft: d}, nil
}

func (s *Service) CreateFromSnapshot(ctx context.Context, a Actor, p Prepared, selected int) (ListView, error) {
	if err := s.require(a, calllist.CapabilityManage); err != nil {
		return ListView{}, err
	}
	if !p.preparedFor(a) {
		return ListView{}, calllist.ErrForbidden
	}
	l, err := calllist.NewList(p.draft, selected, s.now())
	if err != nil {
		return ListView{}, err
	}
	stored, existed, err := s.deps.Store.Create(ctx, l)
	if err != nil {
		return ListView{}, err
	}
	if !existed {
		s.launch(calllist.BuildRef{WorkspaceID: stored.WorkspaceID, ID: stored.ID})
	}
	return viewOf(stored, true), nil
}

func (s *Service) launch(ref calllist.BuildRef) {
	s.deps.Background(func() { s.build(context.Background(), ref) })
}

func (s *Service) Sweep(ctx context.Context) error {
	if failed, err := s.deps.Store.FailExhausted(ctx, s.now()); err != nil {
		return fmt.Errorf("fail exhausted call list builds: %w", err)
	} else if failed > 0 {
		logf("%d call list builds ran out of attempts", failed)
	}
	refs, err := s.deps.Store.Buildable(ctx, s.now(), sweepBuilds)
	if err != nil {
		return fmt.Errorf("call lists to build: %w", err)
	}
	for _, ref := range refs {
		s.launch(ref)
	}
	return nil
}

func buildPage(choice calllist.PhoneChoice) int {
	if choice.Source == calllist.PhoneContact {
		return contactBuildPage
	}
	return identityBuildPage
}

func (s *Service) build(ctx context.Context, ref calllist.BuildRef) {
	token := s.deps.NewToken()
	l, err := s.deps.Store.ClaimBuild(ctx, ref.WorkspaceID, ref.ID, token, s.now())
	if err != nil {
		if !errors.Is(err, calllist.ErrBuildClaimLost) {
			logf("claim the build of list %s: %v", ref.ID, err)
		}
		return
	}
	if err := s.fill(ctx, l, token); err != nil {
		logf("build of list %s stopped and is resumed by the sweep: %v", l.ID, err)
		return
	}
	built, err := s.deps.Store.Get(ctx, l.WorkspaceID, l.ID)
	if err != nil {
		logf("read the built list %s: %v", l.ID, err)
		return
	}
	status, failure := calllist.StatusActive, ""
	if built.ItemCount == 0 {
		status, failure = calllist.StatusFailed, calllist.FailureEmpty
	}
	if err := s.deps.Store.FinishBuild(ctx, l.WorkspaceID, l.ID, token, status, failure, s.now()); err != nil {
		logf("finish the build of list %s: %v", l.ID, err)
		return
	}
	if err := s.deps.Snapshots.DropSnapshot(context.WithoutCancel(ctx), l.WorkspaceID, l.ID); err != nil {
		logf("the frozen selection of list %s stays until the sweep: %v", l.ID, err)
	}
}

func (s *Service) fill(ctx context.Context, l *calllist.List, token string) error {
	dial := l.DialContext()
	cursor := l.BuildCursor
	for {
		ids, err := s.deps.Snapshots.SnapshotPage(ctx, l.WorkspaceID, l.ID, cursor, buildPage(l.Phone))
		if err != nil {
			return fmt.Errorf("read the frozen selection: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		batch, err := s.admit(ctx, l, dial, ids)
		if err != nil {
			return err
		}
		batch.Claim, batch.Cursor, batch.At = token, ids[len(ids)-1], s.now()
		if err := s.deps.Store.AppendItems(ctx, batch); err != nil {
			return err
		}
		cursor = batch.Cursor
	}
}

func (s *Service) admit(ctx context.Context, l *calllist.List, dial lead.DialContext, ids []string) (calllist.BuildBatch, error) {
	leads, err := s.deps.Leads.LoadManyForDial(ctx, l.WorkspaceID, ids)
	if err != nil {
		return calllist.BuildBatch{}, fmt.Errorf("leads of the frozen selection: %w", err)
	}
	byID := make(map[string]*lead.Lead, len(leads))
	numbers := make([]string, 0, len(leads))
	for _, ld := range leads {
		byID[ld.ID] = ld
		if number := l.Phone.Pick(ld); number != "" {
			numbers = append(numbers, number)
		}
	}
	var identities []*lead.Lead
	if l.Phone.Source == calllist.PhoneContact && len(numbers) > 0 {
		if identities, err = s.deps.Leads.FindIdentities(ctx, l.WorkspaceID, numbers); err != nil {
			return calllist.BuildBatch{}, fmt.Errorf("WhatsApp holders of the chosen numbers: %w", err)
		}
	}
	batch := calllist.BuildBatch{WorkspaceID: l.WorkspaceID, ListID: l.ID, Skipped: calllist.Skips{}}
	for _, id := range ids {
		phone, skip := calllist.Admission(byID[id], l.Phone, identities, dial)
		if skip != "" {
			batch.Skipped.Add(skip)
			continue
		}
		batch.Items = append(batch.Items, calllist.Item{LeadID: id, Phone: phone})
	}
	return batch, nil
}

type ListView struct {
	*calllist.List
	Verdict calllist.ListVerdict
}

type ListViews struct {
	Lists []ListView
	Total int64
}

func viewOf(l *calllist.List, manages bool) ListView {
	return ListView{List: l, Verdict: l.Verdict(manages)}
}

func (s *Service) Lists(ctx context.Context, a Actor, q calllist.ListQuery) (ListViews, error) {
	if err := s.require(a, calllist.CapabilityView); err != nil {
		return ListViews{}, err
	}
	q.WorkspaceID, q.ViewerID, q.Manages = a.WorkspaceID, a.UserID, s.holds(a, calllist.CapabilityManage)
	page, err := s.deps.Store.Page(ctx, q.Normalized())
	if err != nil {
		return ListViews{}, err
	}
	views := ListViews{Lists: make([]ListView, 0, len(page.Lists)), Total: page.Total}
	for _, l := range page.Lists {
		views.Lists = append(views.Lists, viewOf(l, q.Manages))
	}
	return views, nil
}

func (s *Service) View(ctx context.Context, a Actor, id string) (ListView, error) {
	l, err := s.Get(ctx, a, id)
	if err != nil {
		return ListView{}, err
	}
	return viewOf(l, s.holds(a, calllist.CapabilityManage)), nil
}

func (s *Service) Get(ctx context.Context, a Actor, id string) (*calllist.List, error) {
	if err := s.require(a, calllist.CapabilityView); err != nil {
		return nil, err
	}
	l, err := s.deps.Store.Get(ctx, a.WorkspaceID, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if !l.VisibleTo(a.UserID, s.holds(a, calllist.CapabilityManage)) {
		return nil, calllist.ErrListNotFound
	}
	return l, nil
}

func (s *Service) Update(ctx context.Context, a Actor, id string, c calllist.Change) (ListView, error) {
	if err := s.require(a, calllist.CapabilityManage); err != nil {
		return ListView{}, err
	}
	l, err := s.deps.Store.Get(ctx, a.WorkspaceID, strings.TrimSpace(id))
	if err != nil {
		return ListView{}, err
	}
	before := l.AssigneeIDs
	c.By = a.UserID
	if err := l.Change(c, s.now()); err != nil {
		return ListView{}, err
	}
	if c.AssigneeIDs != nil {
		if err := s.checkAssignees(l.WorkspaceID, added(before, l.AssigneeIDs)); err != nil {
			return ListView{}, err
		}
	}
	if err := s.deps.Store.Update(ctx, l); err != nil {
		return ListView{}, err
	}
	return viewOf(l, true), nil
}

func added(before, after []string) []string {
	known := make(map[string]bool, len(before))
	for _, id := range before {
		known[id] = true
	}
	out := []string{}
	for _, id := range after {
		if !known[id] {
			out = append(out, id)
		}
	}
	return out
}

func (s *Service) Delete(ctx context.Context, a Actor, id string) error {
	if err := s.require(a, calllist.CapabilityManage); err != nil {
		return err
	}
	return s.deps.Store.Delete(ctx, a.WorkspaceID, strings.TrimSpace(id))
}

type ItemRow struct {
	calllist.ItemView
	Outcome  callhistory.Outcome
	Closable bool
}

type ItemRows struct {
	Items  []ItemRow
	Next   int
	NextAt *time.Time
	AsOf   *time.Time
}

func (s *Service) Items(ctx context.Context, a Actor, q calllist.ItemQuery) (ItemRows, error) {
	l, err := s.Get(ctx, a, q.ListID)
	if err != nil {
		return ItemRows{}, err
	}
	q.WorkspaceID, q.ListID = a.WorkspaceID, l.ID
	if q.Agenda() && !q.Continues() && q.AsOf.IsZero() {
		q.AsOf = s.now()
	}
	q = q.Normalized()
	if err := q.Check(); err != nil {
		return ItemRows{}, err
	}
	page, err := s.deps.Store.Items(ctx, q)
	if err != nil {
		return ItemRows{}, err
	}
	works := s.holds(a, calllist.CapabilityWork)
	rows := ItemRows{Items: make([]ItemRow, 0, len(page.Items)), Next: page.Next, NextAt: page.NextAt, AsOf: page.AsOf}
	for _, view := range page.Items {
		row := ItemRow{ItemView: view, Closable: works && l.Closable(view.Item, a.UserID, view.StampedCall, s.now())}
		if view.LastCall != nil {
			row.Outcome = callhistory.OutcomeOf(*view.LastCall)
		}
		rows.Items = append(rows.Items, row)
	}
	return rows, nil
}

type NextResult struct {
	List            *calllist.List
	Item            *calllist.Item
	Lead            calllist.LeadCard
	LastInteraction *calllist.LastInteraction
	Trunks          []sip_trunk.TrunkChoice
	TrunkRefusal    lead_usecase.TrunkRefusal
	Refused         int
	More            bool
	Verdict         calllist.ListVerdict
	Closable        bool
}

func (s *Service) Next(ctx context.Context, a Actor, listID string) (*NextResult, error) {
	if err := s.require(a, calllist.CapabilityWork); err != nil {
		return nil, err
	}
	l, err := s.deps.Store.Get(ctx, a.WorkspaceID, strings.TrimSpace(listID))
	if err != nil {
		return nil, err
	}
	if err := l.Workable(a.UserID); err != nil {
		return nil, err
	}
	dial := l.DialContext()
	result := &NextResult{List: l, Verdict: l.Verdict(s.holds(a, calllist.CapabilityManage))}
	for attempt := 0; attempt < maxRefusedPerNext; attempt++ {
		item, err := s.deps.Store.Next(ctx, calllist.NextClaim{WorkspaceID: l.WorkspaceID, ListID: l.ID, UserID: a.UserID, Now: s.now()})
		if err != nil || item == nil {
			return result, err
		}
		served, reason, err := s.serve(ctx, a, item, dial, result)
		if err != nil {
			return nil, err
		}
		if served {
			return result, nil
		}
		if err := s.refuse(ctx, l.WorkspaceID, item.ID, reason); err != nil {
			return nil, err
		}
		result.Refused++
	}
	result.More = true
	return result, nil
}

func (s *Service) serve(ctx context.Context, a Actor, item *calllist.Item, dial lead.DialContext, result *NextResult) (bool, calllist.SkipReason, error) {
	ld, err := s.deps.Leads.Load(ctx, item.WorkspaceID, item.LeadID)
	if errors.Is(err, lead.ErrLeadNotFound) {
		ld, err = nil, nil
	}
	if err != nil {
		return false, "", fmt.Errorf("lead of call list item %s: %w", item.ID, err)
	}
	var identities []*lead.Lead
	if ld != nil {
		if identities, err = s.deps.Leads.FindIdentities(ctx, item.WorkspaceID, []string{item.Phone}); err != nil {
			return false, "", fmt.Errorf("WhatsApp holder of the number of item %s: %w", item.ID, err)
		}
	}
	if reason := calllist.Readmit(ld, item.Phone, identities, dial); reason != "" {
		return false, reason, nil
	}
	plan, err := s.deps.Lines.NumberLines(ctx, a, item.Phone)
	if errors.Is(err, sip_trunk.ErrInvalidPhoneNumber) {
		return false, calllist.SkipReason(lead.DialRefusedInvalidNumber), nil
	}
	if err != nil {
		return false, "", err
	}
	result.Item, result.Lead = item, calllist.CardOf(ld)
	result.Trunks, result.TrunkRefusal = plan.Trunks, plan.TrunkRefusal
	result.LastInteraction = s.lastInteraction(ctx, a, ld.ID)
	result.Closable = result.List.Closable(*item, a.UserID, s.shownCall(item), s.now())
	return true, "", nil
}

func (s *Service) lastInteraction(ctx context.Context, a Actor, leadID string) *calllist.LastInteraction {
	latest, err := s.deps.Interactions.Latest(ctx, a.WorkspaceID, leadID)
	if err != nil {
		logf("latest interaction of lead %s left out: %v", leadID, err)
		return nil
	}
	if latest == nil {
		return nil
	}
	person := shared.Person{UserID: a.UserID, SystemAdmin: a.IsAdmin}
	if !person.MayActOn(s.deps.Access, a.WorkspaceID, latest.EntryID, string(latest.EntryType)) {
		return nil
	}
	return latest
}

func (s *Service) refuse(ctx context.Context, workspaceID, itemID string, reason calllist.SkipReason) error {
	_, err := s.deps.Store.Mutate(ctx, workspaceID, itemID, func(i *calllist.Item, _ *calllist.List) error {
		_, err := i.Refuse(reason, s.now())
		return err
	})
	return err
}

type ItemVerdict struct {
	*calllist.Item
	Closable bool
}

func (s *Service) Release(ctx context.Context, a Actor, itemID string) (ItemVerdict, error) {
	if err := s.require(a, calllist.CapabilityWork); err != nil {
		return ItemVerdict{}, err
	}
	item, err := s.deps.Store.Item(ctx, a.WorkspaceID, strings.TrimSpace(itemID))
	if err != nil {
		return ItemVerdict{}, err
	}
	call := s.shownCall(item)
	closable := false
	released, err := s.deps.Store.Mutate(ctx, a.WorkspaceID, item.ID, func(i *calllist.Item, l *calllist.List) error {
		if err := i.Release(a.UserID, s.now()); err != nil {
			return err
		}
		closable = l.Closable(*i, a.UserID, call, s.now())
		return nil
	})
	if err != nil {
		return ItemVerdict{}, err
	}
	return ItemVerdict{Item: released, Closable: closable}, nil
}

func (s *Service) stampedCall(item *calllist.Item) (*calllist.CallFacts, error) {
	if item.LastCallID == "" {
		return nil, nil
	}
	call, err := s.deps.Calls.GetByID(item.LastCallID)
	if errors.Is(err, cdr.ErrCallNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("call %s of item %s: %w", item.LastCallID, item.ID, err)
	}
	return calllist.FactsOf(call), nil
}

func (s *Service) shownCall(item *calllist.Item) *calllist.CallFacts {
	call, err := s.stampedCall(item)
	if err != nil {
		logf("item %s is shown as not closable: %v", item.ID, err)
		return nil
	}
	return call
}

func (s *Service) Close(ctx context.Context, a Actor, itemID string, c calllist.Closing) (ItemVerdict, error) {
	if err := s.require(a, calllist.CapabilityWork); err != nil {
		return ItemVerdict{}, err
	}
	item, err := s.deps.Store.Item(ctx, a.WorkspaceID, strings.TrimSpace(itemID))
	if err != nil {
		return ItemVerdict{}, err
	}
	call, err := s.stampedCall(item)
	if err != nil {
		return ItemVerdict{}, err
	}
	catalogue, err := s.deps.Outcomes.OutcomeCaptureFor(ctx, a.WorkspaceID)
	if err != nil {
		return ItemVerdict{}, fmt.Errorf("outcome catalogue of workspace %s: %w", a.WorkspaceID, err)
	}
	c.By = a.UserID
	closable := false
	closed, err := s.deps.Store.Mutate(ctx, a.WorkspaceID, item.ID, func(i *calllist.Item, l *calllist.List) error {
		if err := l.AcceptsOutcomes(); err != nil {
			return err
		}
		if _, err := i.Close(c, call, catalogue, s.now()); err != nil {
			return err
		}
		closable = l.Closable(*i, a.UserID, call, s.now())
		return nil
	})
	if err != nil {
		return ItemVerdict{}, err
	}
	return ItemVerdict{Item: closed, Closable: closable}, nil
}

func (s *Service) CheckItemDial(ctx context.Context, dial callsession.CallListItemDial) error {
	item, err := s.deps.Store.Item(ctx, dial.WorkspaceID, strings.TrimSpace(dial.ItemID))
	if err != nil {
		return err
	}
	l, err := s.deps.Store.Get(ctx, dial.WorkspaceID, item.ListID)
	if err != nil {
		return err
	}
	if err := l.Workable(dial.UserID); err != nil {
		return err
	}
	if err := item.CheckDial(dial.UserID, dial.LeadID, dial.Number, s.now()); err != nil {
		return err
	}
	return nil
}

func (s *Service) StampLastCall(ctx context.Context, stamp callsession.CallListItemStamp) error {
	if strings.TrimSpace(stamp.CallRecordID) == "" {
		return fmt.Errorf("%w: %w", callsession.ErrCallListStampRefused, calllist.ErrCallNotTheItems)
	}
	_, err := s.deps.Store.Mutate(ctx, stamp.WorkspaceID, strings.TrimSpace(stamp.ItemID), func(i *calllist.Item, _ *calllist.List) error {
		if err := i.Stamp(stamp.UserID, stamp.CallRecordID, s.now()); err != nil {
			return fmt.Errorf("%w: %w", callsession.ErrCallListStampRefused, err)
		}
		return nil
	})
	return err
}
