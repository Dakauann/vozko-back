package lead_usecase

import (
	"context"
	"errors"
	"fmt"

	"vozko/domain/actor"
	"vozko/domain/callrouting"
	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/calllist"
	"vozko/domain/lead"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

const timelineReads = 4

var errTimelineIncomplete = errors.New("lead timeline: a required dependency is missing")

type DealScopes interface {
	Scope(by shared.Person, workspaceID string) (opportunity.DealScope, error)
}

type CallAccess interface {
	Execute(userID, workspaceID string, resource workspace.Resource, action workspace.Action) error
}

type TimelineDeps struct {
	Leads       LeadFinder
	Source      lead.TimelineSource
	Deals       opportunity.LeadDealReader
	Access      EntryAccessResolver
	Permissions Permissions
	Definitions DefinitionSource
	CallAccess  CallAccess
	Transfers   callrouting.TransferHistory
	DealScopes  DealScopes
	Names       actor.Namer
}

type Timeline struct {
	deps    TimelineDeps
	viewers viewers
}

type DealsPage struct {
	Deals []*opportunity.Opportunity
	Next  string
}

func NewTimeline(deps TimelineDeps) (*Timeline, error) {
	missing := map[string]bool{
		"leads":       deps.Leads == nil,
		"source":      deps.Source == nil,
		"deals":       deps.Deals == nil,
		"access":      deps.Access == nil,
		"permissions": deps.Permissions == nil,
		"call access": deps.CallAccess == nil,
		"transfers":   deps.Transfers == nil,
		"deal scopes": deps.DealScopes == nil,
		"names":       deps.Names == nil,
		"definitions": deps.Definitions == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errTimelineIncomplete, name)
		}
	}
	return &Timeline{deps: deps, viewers: viewers{permissions: deps.Permissions, definitions: deps.Definitions}}, nil
}

type callReader struct {
	seesEveryone bool
	lists        calllist.Viewer
}

type timelineReader struct {
	actor  Actor
	fields lead.Viewer
	calls  *callReader
	deals  *opportunity.DealScope
}

func (t *Timeline) Page(ctx context.Context, a Actor, q lead.PageQuery) (lead.TimelinePage, error) {
	q, before, err := t.open(a, q, lead.PageQuery.Cursor)
	if err != nil {
		return lead.TimelinePage{}, err
	}
	l, err := found(t.deps.Leads.Load(ctx, a.WorkspaceID, q.LeadID))
	if err != nil {
		return lead.TimelinePage{}, err
	}
	reader, err := t.reader(a)
	if err != nil {
		return lead.TimelinePage{}, err
	}
	scan := lead.TimelineScan{
		WorkspaceID: a.WorkspaceID,
		LeadID:      l.ID,
		NumberForms: l.LegacyCallForms(),
		Limit:       q.Limit + 1,
		Calls:       reader.calls != nil,
		Deals:       reader.deals,
	}
	visible := make([]lead.TimelineItem, 0, q.Limit)
	var last shared.Keyset
	for read := 0; read < timelineReads; read++ {
		scan.Before = before
		batch, err := t.deps.Source.ScanTimeline(ctx, scan)
		if err != nil {
			return lead.TimelinePage{}, fmt.Errorf("timeline of lead %s: %w", l.ID, err)
		}
		grants, err := t.grants(ctx, reader, batch)
		if err != nil {
			return lead.TimelinePage{}, err
		}
		for _, item := range batch {
			if len(visible) == q.Limit {
				return t.page(visible, true), nil
			}
			last = item.Cursor()
			if shown, ok := grants.Visible(item); ok {
				visible = append(visible, shown)
			}
		}
		if len(batch) <= q.Limit {
			return t.page(visible, false), nil
		}
		if len(visible) == q.Limit {
			return t.page(visible, true), nil
		}
		before = &last
	}
	page := t.page(visible, false)
	page.Next = last.Encode()
	return page, nil
}

func (t *Timeline) page(items []lead.TimelineItem, more bool) lead.TimelinePage {
	lead.NameTimelineActors(t.deps.Names, items)
	page := lead.TimelinePage{Items: items}
	if more && len(items) > 0 {
		page.Next = items[len(items)-1].Cursor().Encode()
	}
	return page
}

func (t *Timeline) Deals(ctx context.Context, a Actor, q lead.PageQuery) (DealsPage, error) {
	q, before, err := t.open(a, q, lead.PageQuery.DealsCursor)
	if err != nil {
		return DealsPage{}, err
	}
	l, err := found(t.deps.Leads.FindByID(a.WorkspaceID, q.LeadID))
	if err != nil {
		return DealsPage{}, err
	}
	scope, err := t.deps.DealScopes.Scope(personOf(a), a.WorkspaceID)
	if err != nil {
		return DealsPage{}, err
	}
	deals, err := t.deps.Deals.DealsOfLead(ctx, opportunity.LeadDealsQuery{
		WorkspaceID: a.WorkspaceID, LeadID: l.ID, Scope: scope, Before: before, Limit: q.Limit + 1,
	})
	if err != nil {
		return DealsPage{}, fmt.Errorf("deals of lead %s: %w", l.ID, err)
	}
	page := DealsPage{Deals: deals}
	if len(deals) > q.Limit {
		page.Deals = deals[:q.Limit]
		oldest := page.Deals[q.Limit-1]
		page.Next = shared.Keyset{At: oldest.CreatedAt, ID: oldest.ID}.Encode()
	}
	opportunity.NameParticipants(t.deps.Names, page.Deals...)
	return page, nil
}

func (t *Timeline) open(a Actor, q lead.PageQuery, cursor func(lead.PageQuery) (*shared.Keyset, error)) (lead.PageQuery, *shared.Keyset, error) {
	if err := requireLeadRef(a, q.LeadID); err != nil {
		return lead.PageQuery{}, nil, err
	}
	q, err := q.Normalize()
	if err != nil {
		return lead.PageQuery{}, nil, err
	}
	if !t.viewers.allowed(a, workspace.ActionRead) {
		return lead.PageQuery{}, nil, lead.ErrLeadForbidden
	}
	before, err := cursor(q)
	if err != nil {
		return lead.PageQuery{}, nil, err
	}
	return q, before, nil
}

func personOf(a Actor) shared.Person {
	return shared.Person{UserID: a.UserID, SystemAdmin: a.IsAdmin}
}

func (t *Timeline) reader(a Actor) (timelineReader, error) {
	fields, err := t.viewers.of(a)
	if err != nil {
		return timelineReader{}, err
	}
	reader := timelineReader{actor: a, fields: fields}
	may := func(action workspace.Action) bool {
		return t.deps.CallAccess.Execute(a.UserID, a.WorkspaceID, workspace.ResourceCallHistory, action) == nil
	}
	if may(workspace.ActionRead) {
		reader.calls = &callReader{
			seesEveryone: may(workspace.ActionViewOthers),
			lists:        calllist.ViewerOf(t.deps.Permissions, a.WorkspaceID, a.UserID, a.IsAdmin),
		}
	}
	scope, err := t.deps.DealScopes.Scope(personOf(a), a.WorkspaceID)
	switch {
	case errors.Is(err, opportunity.ErrScopeDenied):
	case err != nil:
		return timelineReader{}, fmt.Errorf("deal scope: %w", err)
	default:
		reader.deals = &scope
	}
	return reader, nil
}

func (t *Timeline) grants(ctx context.Context, reader timelineReader, items []lead.TimelineItem) (lead.TimelineGrants, error) {
	entries, err := visibleEntries(t.deps.Access, reader.actor, lead.TimelineEntries(items))
	if err != nil {
		return lead.TimelineGrants{}, err
	}
	calls, err := t.visibleCalls(ctx, reader, items)
	if err != nil {
		return lead.TimelineGrants{}, err
	}
	return lead.TimelineGrants{Entries: entries, Calls: calls, CallLists: visibleCallLists(reader, items), Deals: reader.deals != nil, Fields: reader.fields}, nil
}

func visibleCallLists(reader timelineReader, items []lead.TimelineItem) map[string]bool {
	visible := map[string]bool{}
	if reader.calls == nil {
		return visible
	}
	for listID, assignees := range lead.TimelineCallLists(items) {
		if reader.calls.lists.Sees(assignees) {
			visible[listID] = true
		}
	}
	return visible
}

func (t *Timeline) visibleCalls(ctx context.Context, reader timelineReader, items []lead.TimelineItem) (map[string]bool, error) {
	visible := map[string]bool{}
	ids := lead.TimelineCalls(items)
	if reader.calls == nil || len(ids) == 0 {
		return visible, nil
	}
	transfers, err := t.deps.Transfers.ForCalls(ctx, reader.actor.WorkspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("call transfers: %w", err)
	}
	byCall := map[string][]callrouting.TransferRecord{}
	for _, transfer := range transfers {
		byCall[transfer.CallID] = append(byCall[transfer.CallID], transfer)
	}
	for _, item := range items {
		if item.Kind != lead.TimelineCall || item.Call == nil || item.Call.CallID != item.Ref.ID {
			continue
		}
		if callhistory.Visible(reader.actor.UserID, reader.calls.seesEveryone, *item.Call, byCall[item.Ref.ID]) {
			visible[item.Ref.ID] = true
		}
	}
	return visible, nil
}
