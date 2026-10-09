package lead_usecase

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/calls/cdr"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

type timelineSource struct {
	items []lead.TimelineItem
	scans []lead.TimelineScan
	err   error
}

func (s *timelineSource) ScanTimeline(_ context.Context, scan lead.TimelineScan) ([]lead.TimelineItem, error) {
	s.scans = append(s.scans, scan)
	if s.err != nil {
		return nil, s.err
	}
	var out []lead.TimelineItem
	for _, item := range s.items {
		if item.Kind == lead.TimelineCall && !scan.Calls {
			continue
		}
		if item.Kind == lead.TimelineDeal && scan.Deals == nil {
			continue
		}
		if scan.Before != nil && !olderThan(item, *scan.Before) {
			continue
		}
		out = append(out, item)
		if len(out) == scan.Limit {
			break
		}
	}
	return out, nil
}

func olderThan(item lead.TimelineItem, k shared.Keyset) bool {
	return item.At.Before(k.At) || (item.At.Equal(k.At) && item.ID < k.ID)
}

type callGrants map[string]map[workspace.PermissionEntry]bool

func (g callGrants) Execute(userID, _ string, resource workspace.Resource, action workspace.Action) error {
	if g[userID][workspace.PermissionEntry{Resource: resource, Action: action}] {
		return nil
	}
	return workspace.ErrInsufficientPermissions
}

type callTransfers struct {
	byCall map[string][]callrouting.TransferRecord
	asked  [][]string
	err    error
}

func (c *callTransfers) ForCalls(_ context.Context, _ string, callIDs []string) ([]callrouting.TransferRecord, error) {
	c.asked = append(c.asked, callIDs)
	if c.err != nil {
		return nil, c.err
	}
	var out []callrouting.TransferRecord
	for _, id := range callIDs {
		out = append(out, c.byCall[id]...)
	}
	return out, nil
}

type dealScopes struct {
	byUser map[string]opportunity.DealScope
	err    error
}

func (d dealScopes) Scope(by shared.Person, _ string) (opportunity.DealScope, error) {
	if d.err != nil {
		return opportunity.DealScope{}, d.err
	}
	scope, ok := d.byUser[by.UserID]
	if !ok {
		return opportunity.DealScope{}, opportunity.ErrScopeDenied
	}
	return scope, nil
}

type leadDeals struct {
	deals []*opportunity.Opportunity
	asked []opportunity.LeadDealsQuery
	err   error
}

func (d *leadDeals) DealsOfLead(_ context.Context, q opportunity.LeadDealsQuery) ([]*opportunity.Opportunity, error) {
	d.asked = append(d.asked, q)
	if d.err != nil {
		return nil, d.err
	}
	var out []*opportunity.Opportunity
	for _, o := range d.deals {
		if q.Before != nil && !(o.CreatedAt.Before(q.Before.At) || (o.CreatedAt.Equal(q.Before.At) && o.ID < q.Before.ID)) {
			continue
		}
		out = append(out, o)
		if len(out) == q.Limit {
			break
		}
	}
	return out, nil
}

type timelineNames map[string]string

func (n timelineNames) Names(ids ...string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := n[id]; ok {
			out[id] = name
		}
	}
	return out
}

const (
	callManager = "u-manager"
	noCalls     = "u-no-calls"
)

var (
	timelineStart = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	callRead      = workspace.PermissionEntry{Resource: workspace.ResourceCallHistory, Action: workspace.ActionRead}
	callOthers    = workspace.PermissionEntry{Resource: workspace.ResourceCallHistory, Action: workspace.ActionViewOthers}
)

type timelineFixture struct {
	timeline  *Timeline
	source    *timelineSource
	access    *accessResolver
	transfers *callTransfers
	deals     *leadDeals
}

func entryItem(id string, kind lead.TimelineKind, minutes int, entryID string, entryType shared.EntryType) lead.TimelineItem {
	return lead.TimelineItem{
		ID: id, Kind: kind, At: timelineStart.Add(-time.Duration(minutes) * time.Minute),
		Ref: lead.TimelineRef{Type: lead.TimelineRefEntry, ID: entryID, EntryType: entryType},
	}
}

func callItem(id string, minutes int, agent string) lead.TimelineItem {
	return lead.TimelineItem{
		ID: "call:" + id, Kind: lead.TimelineCall, At: timelineStart.Add(-time.Duration(minutes) * time.Minute), Actor: agent,
		Ref:     lead.TimelineRef{Type: lead.TimelineRefCall, ID: id},
		Summary: lead.TimelineSummary{Direction: string(cdr.DirectionOutbound)},
		Call:    &cdr.Call{CallID: id, Direction: cdr.DirectionOutbound, AgentID: &agent},
	}
}

func plainItem(id string, kind lead.TimelineKind, ref lead.TimelineRefType, minutes int, actor string) lead.TimelineItem {
	return lead.TimelineItem{
		ID: id, Kind: kind, At: timelineStart.Add(-time.Duration(minutes) * time.Minute), Actor: actor,
		Ref: lead.TimelineRef{Type: ref, ID: id},
	}
}

func everyKindOfItem() []lead.TimelineItem {
	return []lead.TimelineItem{
		entryItem("conversation:whatsapp:e-sales", lead.TimelineConversation, 1, "e-sales", shared.EntryTypeWhatsApp),
		entryItem("conversation:instagram:e-support", lead.TimelineConversation, 2, "e-support", shared.EntryTypeInstagram),
		entryItem("campaign_read:e-sales", lead.TimelineCampaignRead, 3, "e-sales", shared.EntryTypeWhatsApp),
		entryItem("campaign_sent:u-1", lead.TimelineCampaignSent, 4, "", shared.EntryTypeUnofficialWhatsApp),
		callItem("call-mine", 5, salesMember),
		callItem("call-theirs", 6, supportMember),
		callItem("call-passed", 7, supportMember),
		plainItem("deal:d-1", lead.TimelineDeal, lead.TimelineRefDeal, 8, salesMember),
		plainItem("memory:m-1", lead.TimelineMemory, lead.TimelineRefMemory, 9, "ai:agent-1"),
		plainItem("record:ev-1", lead.TimelineRecord, lead.TimelineRefEvent, 10, salesMember),
		plainItem("sms:x", "sms", lead.TimelineRefEntry, 11, ""),
	}
}

func newTimelineFixture(t *testing.T, items []lead.TimelineItem) *timelineFixture {
	t.Helper()
	return newTimelineFixtureWith(t, items, fakePermissions{"leads:read": true})
}

func newTimelineFixtureWith(t *testing.T, items []lead.TimelineItem, permissions Permissions) *timelineFixture {
	t.Helper()
	f := &timelineFixture{
		source: &timelineSource{items: items},
		access: &accessResolver{byUser: map[string]map[string]bool{
			salesMember: {"e-sales": true},
			callManager: {"e-sales": true, "e-support": true},
			noCalls:     {"e-sales": true},
		}},
		transfers: &callTransfers{byCall: map[string][]callrouting.TransferRecord{
			"call-passed": {{CallID: "call-passed", FromUserID: supportMember, Target: callrouting.TransferTarget{UserID: salesMember}}},
		}},
		deals: &leadDeals{},
	}
	timeline, err := NewTimeline(TimelineDeps{
		Leads:  historyLeads{"l-1": {ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321"}},
		Source: f.source, Deals: f.deals, Access: f.access,
		Permissions: permissions,
		Definitions: &fakeDefinitions{defs: leadDefinitions()},
		CallAccess: callGrants{
			salesMember: {callRead: true},
			callManager: {callRead: true, callOthers: true},
		},
		Transfers:  f.transfers,
		DealScopes: dealScopes{byUser: map[string]opportunity.DealScope{salesMember: {Restrict: true, DepartmentIDs: []string{"dep-sales"}, AssigneeOverride: salesMember}, callManager: {}}},
		Names:      timelineNames{salesMember: "Sara", "ai:agent-1": "Elo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.timeline = timeline
	return f
}

func timelineViewer(userID string) Actor {
	return Actor{UserID: userID, WorkspaceID: "ws-1"}
}

func itemIDs(items []lead.TimelineItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestTheTimelineLeavesOutEveryItemTheViewerCannotOpen(t *testing.T) {
	f := newTimelineFixture(t, everyKindOfItem())
	page, err := f.timeline.Page(context.Background(), timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"conversation:whatsapp:e-sales", "campaign_read:e-sales", "call:call-mine", "call:call-passed",
		"deal:d-1", "memory:m-1", "record:ev-1",
	}
	if got := itemIDs(page.Items); !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if page.Next != "" {
		t.Fatalf("the whole history fit, next = %q", page.Next)
	}
	if f.access.checks != 1 || len(f.transfers.asked) != 1 {
		t.Fatalf("access read %d times, transfers %d times; both must be one batch", f.access.checks, len(f.transfers.asked))
	}
	if names := []string{page.Items[0].ActorName, page.Items[2].ActorName, page.Items[5].ActorName}; !reflect.DeepEqual(names, []string{"", "Sara", "Elo"}) {
		t.Fatalf("actor names = %v", names)
	}
}

func TestTheScanAsksOnlyForTheSourcesTheViewerMayRead(t *testing.T) {
	cases := []struct {
		name      string
		viewer    string
		wantCalls bool
		wantDeals *opportunity.DealScope
	}{
		{"a seller with calls and scoped deals", salesMember, true, &opportunity.DealScope{Restrict: true, DepartmentIDs: []string{"dep-sales"}, AssigneeOverride: salesMember}},
		{"a manager with every call and every deal", callManager, true, &opportunity.DealScope{}},
		{"a member without call history nor deals", noCalls, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTimelineFixture(t, everyKindOfItem())
			page, err := f.timeline.Page(context.Background(), timelineViewer(tc.viewer), lead.PageQuery{LeadID: "l-1", Limit: 5})
			if err != nil {
				t.Fatal(err)
			}
			scan := f.source.scans[0]
			if scan.Calls != tc.wantCalls || !reflect.DeepEqual(scan.Deals, tc.wantDeals) {
				t.Fatalf("scan calls %v deals %+v", scan.Calls, scan.Deals)
			}
			if scan.WorkspaceID != "ws-1" || scan.LeadID != "l-1" || scan.Limit != 6 || scan.Before != nil {
				t.Fatalf("scan = %+v", scan)
			}
			if !reflect.DeepEqual(scan.NumberForms, []string{"5511987654321", "551187654321", "+5511987654321", "+551187654321"}) {
				t.Fatalf("number forms = %v", scan.NumberForms)
			}
			for _, item := range page.Items {
				if (item.Kind == lead.TimelineCall && !tc.wantCalls) || (item.Kind == lead.TimelineDeal && tc.wantDeals == nil) {
					t.Fatalf("%s reached a viewer who may not read it", item.ID)
				}
			}
		})
	}
}

func TestAManagerWhoSeesTheTeamGetsEveryCall(t *testing.T) {
	f := newTimelineFixture(t, everyKindOfItem())
	page, err := f.timeline.Page(context.Background(), timelineViewer(callManager), lead.PageQuery{LeadID: "l-1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"conversation:whatsapp:e-sales", "conversation:instagram:e-support", "campaign_read:e-sales",
		"call:call-mine", "call:call-theirs", "call:call-passed", "deal:d-1", "memory:m-1", "record:ev-1",
	}
	if got := itemIDs(page.Items); !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
}

func longHistory() ([]lead.TimelineItem, []string) {
	var items []lead.TimelineItem
	var visible []string
	for i := 0; i < 57; i++ {
		entry, id := "e-support", fmt.Sprintf("conversation:whatsapp:%03d", 999-i)
		if i%3 == 0 || i > 40 {
			entry = "e-sales"
			visible = append(visible, id)
		}
		items = append(items, entryItem(id, lead.TimelineConversation, i/2, entry, shared.EntryTypeWhatsApp))
	}
	return items, visible
}

func TestTimelinePagesAreStableAndLoseNothing(t *testing.T) {
	items, visible := longHistory()
	f := newTimelineFixture(t, items)
	var got []string
	before := ""
	for pages := 0; ; pages++ {
		if pages > 40 {
			t.Fatal("the pages never ended")
		}
		page, err := f.timeline.Page(context.Background(), timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1", Before: before, Limit: 4})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) > 4 {
			t.Fatalf("page of %d items", len(page.Items))
		}
		got = append(got, itemIDs(page.Items)...)
		if page.Next == "" {
			break
		}
		before = page.Next
	}
	if !reflect.DeepEqual(got, visible) {
		t.Fatalf("pages gave %v, want %v", got, visible)
	}
}

func TestALongRunOfHiddenItemsStopsAfterAFewReadsAndHandsBackACursor(t *testing.T) {
	var items []lead.TimelineItem
	for i := 0; i < 100; i++ {
		items = append(items, entryItem(fmt.Sprintf("conversation:whatsapp:%03d", 999-i), lead.TimelineConversation, i, "e-support", shared.EntryTypeWhatsApp))
	}
	items = append(items, entryItem("conversation:whatsapp:000", lead.TimelineConversation, 200, "e-sales", shared.EntryTypeWhatsApp))
	f := newTimelineFixture(t, items)
	page, err := f.timeline.Page(context.Background(), timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.Next == "" {
		t.Fatalf("page = %d items, next %q", len(page.Items), page.Next)
	}
	if len(f.source.scans) != timelineReads {
		t.Fatalf("read %d times, want %d", len(f.source.scans), timelineReads)
	}
	var found []string
	for before := page.Next; before != ""; {
		next, err := f.timeline.Page(context.Background(), timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1", Limit: 5, Before: before})
		if err != nil {
			t.Fatal(err)
		}
		found = append(found, itemIDs(next.Items)...)
		before = next.Next
	}
	if !reflect.DeepEqual(found, []string{"conversation:whatsapp:000"}) {
		t.Fatalf("the visible item behind the hidden run came back as %v", found)
	}
}

func TestTheTimelineRefusesInsteadOfShowingTooMuch(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		mutate func(*timelineFixture, *TimelineDeps)
		viewer Actor
		query  lead.PageQuery
		want   error
	}{
		{"no leads:read", func(_ *timelineFixture, d *TimelineDeps) { d.Permissions = fakePermissions{} }, timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1"}, lead.ErrLeadForbidden},
		{"a lead of another workspace", nil, Actor{UserID: salesMember, WorkspaceID: "ws-2"}, lead.PageQuery{LeadID: "l-1"}, lead.ErrLeadNotFound},
		{"no workspace", nil, Actor{UserID: salesMember}, lead.PageQuery{LeadID: "l-1"}, lead.ErrLeadWorkspaceRequired},
		{"a cursor that was not ours", nil, timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1", Before: "x"}, lead.ErrPageQueryInvalid},
		{"the conversation scope cannot be read", func(f *timelineFixture, _ *TimelineDeps) { f.access.err = errors.New("db down") }, timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1"}, nil},
		{"the transfers cannot be read", func(f *timelineFixture, _ *TimelineDeps) { f.transfers.err = errors.New("db down") }, timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1"}, nil},
		{"the history cannot be read", func(f *timelineFixture, _ *TimelineDeps) { f.source.err = errors.New("db down") }, timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1"}, nil},
		{"the deal scope cannot be read", func(_ *timelineFixture, d *TimelineDeps) { d.DealScopes = dealScopes{err: errors.New("db down")} }, timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1"}, nil},
		{"the field definitions cannot be read", func(_ *timelineFixture, d *TimelineDeps) {
			d.Definitions = &fakeDefinitions{err: errors.New("db down")}
		}, timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTimelineFixture(t, everyKindOfItem())
			deps := f.timeline.deps
			if tc.mutate != nil {
				tc.mutate(f, &deps)
			}
			timeline, err := NewTimeline(deps)
			if err != nil {
				t.Fatal(err)
			}
			page, err := timeline.Page(ctx, tc.viewer, tc.query)
			if err == nil {
				t.Fatalf("got %d items, want a refusal", len(page.Items))
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func recordItem(id string, minutes int, fields ...string) lead.TimelineItem {
	item := plainItem(id, lead.TimelineRecord, lead.TimelineRefEvent, minutes, salesMember)
	item.Summary = lead.TimelineSummary{Event: "updated", Fields: fields}
	return item
}

func TestARecordChangeNamesASensitiveFieldOnlyToWhoReadsSensitiveFields(t *testing.T) {
	sensitive := lead.CustomFieldName("classificacao")
	items := []lead.TimelineItem{
		recordItem("record:ev-mixed", 1, lead.FieldName, sensitive),
		recordItem("record:ev-sensitive", 2, sensitive),
		recordItem("record:ev-plain", 3, lead.CustomFieldName("cor")),
	}
	cases := []struct {
		name        string
		permissions fakePermissions
		want        map[string][]string
	}{
		{"without read_sensitive", fakePermissions{"leads:read": true}, map[string][]string{
			"record:ev-mixed": {lead.FieldName}, "record:ev-plain": {lead.CustomFieldName("cor")},
		}},
		{"with read_sensitive", fakePermissions{"leads:read": true, "leads:read_sensitive": true}, map[string][]string{
			"record:ev-mixed": {lead.FieldName, sensitive}, "record:ev-sensitive": {sensitive}, "record:ev-plain": {lead.CustomFieldName("cor")},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTimelineFixture(t, items)
			deps := f.timeline.deps
			deps.Permissions = tc.permissions
			timeline, err := NewTimeline(deps)
			if err != nil {
				t.Fatal(err)
			}
			page, err := timeline.Page(context.Background(), timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1"})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string][]string{}
			for _, item := range page.Items {
				got[item.ID] = item.Summary.Fields
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fields = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestACallWithoutItsFactsIsHiddenEvenFromAManager(t *testing.T) {
	bare := callItem("call-bare", 1, salesMember)
	bare.Call = nil
	f := newTimelineFixture(t, []lead.TimelineItem{bare, callItem("call-mine", 2, salesMember)})
	page, err := f.timeline.Page(context.Background(), timelineViewer(callManager), lead.PageQuery{LeadID: "l-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := itemIDs(page.Items); !reflect.DeepEqual(got, []string{"call:call-mine"}) {
		t.Fatalf("items = %v", got)
	}
}

func TestAMissingVisibilityRefuses(t *testing.T) {
	f := newTimelineFixture(t, everyKindOfItem())
	deps := f.timeline.deps
	deps.Access = nilVisibility{}
	timeline, err := NewTimeline(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := timeline.Page(context.Background(), timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1"}); err == nil {
		t.Fatal("a missing conversation scope must refuse")
	}
}

func TestTheTimelineNeedsEveryDependency(t *testing.T) {
	f := newTimelineFixture(t, nil)
	full := f.timeline.deps
	clears := map[string]func(*TimelineDeps){
		"leads":       func(d *TimelineDeps) { d.Leads = nil },
		"source":      func(d *TimelineDeps) { d.Source = nil },
		"deals":       func(d *TimelineDeps) { d.Deals = nil },
		"access":      func(d *TimelineDeps) { d.Access = nil },
		"permissions": func(d *TimelineDeps) { d.Permissions = nil },
		"call access": func(d *TimelineDeps) { d.CallAccess = nil },
		"transfers":   func(d *TimelineDeps) { d.Transfers = nil },
		"deal scopes": func(d *TimelineDeps) { d.DealScopes = nil },
		"names":       func(d *TimelineDeps) { d.Names = nil },
		"definitions": func(d *TimelineDeps) { d.Definitions = nil },
	}
	for name, clear := range clears {
		deps := full
		clear(&deps)
		if _, err := NewTimeline(deps); err == nil {
			t.Errorf("a timeline without %s was built", name)
		}
	}
}

func dealAt(id string, minutes int) *opportunity.Opportunity {
	return &opportunity.Opportunity{ID: id, WorkspaceID: "ws-1", LeadID: "l-1", OwnerID: salesMember, CreatedAt: timelineStart.Add(-time.Duration(minutes) * time.Minute)}
}

const (
	dealID1 = "00000000-0000-4000-8000-000000000001"
	dealID2 = "00000000-0000-4000-8000-000000000002"
	dealID3 = "00000000-0000-4000-8000-000000000003"
)

func TestTheDealsOfALeadArePagedInsideTheViewersScope(t *testing.T) {
	f := newTimelineFixture(t, nil)
	f.deals.deals = []*opportunity.Opportunity{dealAt(dealID3, 1), dealAt(dealID2, 2), dealAt(dealID1, 3)}
	first, err := f.timeline.Deals(context.Background(), timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Deals) != 2 || first.Deals[0].ID != dealID3 || first.Deals[1].ID != dealID2 || first.Next == "" {
		t.Fatalf("first page = %+v", first)
	}
	if first.Deals[0].OwnerName != "Sara" {
		t.Fatalf("owner name = %q", first.Deals[0].OwnerName)
	}
	asked := f.deals.asked[0]
	want := opportunity.LeadDealsQuery{WorkspaceID: "ws-1", LeadID: "l-1", Limit: 3,
		Scope: opportunity.DealScope{Restrict: true, DepartmentIDs: []string{"dep-sales"}, AssigneeOverride: salesMember}}
	if !reflect.DeepEqual(asked, want) {
		t.Fatalf("asked %+v, want %+v", asked, want)
	}
	second, err := f.timeline.Deals(context.Background(), timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1", Limit: 2, Before: first.Next})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Deals) != 1 || second.Deals[0].ID != dealID1 || second.Next != "" {
		t.Fatalf("second page = %+v", second)
	}
}

func TestTheDealsOfALeadRefuseATimelineCursor(t *testing.T) {
	f := newTimelineFixture(t, nil)
	f.deals.deals = []*opportunity.Opportunity{dealAt(dealID3, 1)}
	timelineCursor := shared.Keyset{At: timelineStart, ID: "deal:" + dealID3}.Encode()
	_, err := f.timeline.Deals(context.Background(), timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1", Before: timelineCursor})
	if !errors.Is(err, lead.ErrPageQueryInvalid) {
		t.Fatalf("err = %v, want ErrPageQueryInvalid", err)
	}
	if len(f.deals.asked) != 0 {
		t.Fatal("deals were read with a cursor that does not point at a deal")
	}
}

func TestTheDealsOfALeadRefuseAViewerWithoutDealAccess(t *testing.T) {
	f := newTimelineFixture(t, nil)
	if _, err := f.timeline.Deals(context.Background(), timelineViewer(noCalls), lead.PageQuery{LeadID: "l-1"}); !errors.Is(err, opportunity.ErrScopeDenied) {
		t.Fatalf("err = %v, want ErrScopeDenied", err)
	}
	if len(f.deals.asked) != 0 {
		t.Fatal("deals were read for a viewer without access")
	}
	deps := f.timeline.deps
	deps.Permissions = fakePermissions{}
	timeline, _ := NewTimeline(deps)
	if _, err := timeline.Deals(context.Background(), timelineViewer(salesMember), lead.PageQuery{LeadID: "l-1"}); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("without leads:read err = %v", err)
	}
	if _, err := f.timeline.Deals(context.Background(), Actor{UserID: salesMember, WorkspaceID: "ws-2"}, lead.PageQuery{LeadID: "l-1"}); !errors.Is(err, lead.ErrLeadNotFound) {
		t.Fatalf("another workspace err = %v", err)
	}
}

type nilVisibility struct{}

func (nilVisibility) EntryVisibilityFor(string, string, bool) conversation.EntryVisibility {
	return nil
}

type permissionsByUser map[string]fakePermissions

func (p permissionsByUser) HasWorkspacePermission(userID, workspaceID, resource, action string, isAdmin bool) bool {
	return p[userID].HasWorkspacePermission(userID, workspaceID, resource, action, isAdmin)
}

func listedCallItem(id string, minutes int, agent, listID string, assignees ...string) lead.TimelineItem {
	item := callItem(id, minutes, agent)
	item.Summary.Disposition, item.Summary.CallListID, item.CallListAssignees = "interessado", listID, assignees
	return item
}

func TestACallsListOutcomeReachesOnlyWhoMaySeeThatList(t *testing.T) {
	items := []lead.TimelineItem{
		listedCallItem("call-mine", 1, salesMember, "list-sales", salesMember),
		listedCallItem("call-theirs", 2, supportMember, "list-support", supportMember),
	}
	member := fakePermissions{"leads:read": true, "call_lists:read": true}
	manager := fakePermissions{"leads:read": true, "call_lists:read": true, "call_lists:manage": true}
	cases := []struct {
		name        string
		viewer      string
		permissions permissionsByUser
		want        map[string]string
	}{
		{"a manager of calls who is on no list", callManager, permissionsByUser{callManager: {"leads:read": true}},
			map[string]string{"call:call-mine": "", "call:call-theirs": ""}},
		{"a manager of calls who manages lists", callManager, permissionsByUser{callManager: manager},
			map[string]string{"call:call-mine": "list-sales", "call:call-theirs": "list-support"}},
		{"an assignee of one list", callManager, permissionsByUser{callManager: member},
			map[string]string{"call:call-mine": "", "call:call-theirs": ""}},
		{"the seller who works the list", salesMember, permissionsByUser{salesMember: member},
			map[string]string{"call:call-mine": "list-sales"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTimelineFixtureWith(t, items, tc.permissions)
			page, err := f.timeline.Page(context.Background(), timelineViewer(tc.viewer), lead.PageQuery{LeadID: "l-1"})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, item := range page.Items {
				got[item.ID] = item.Summary.CallListID
				if (item.Summary.CallListID == "") != (item.Summary.Disposition == "") || item.CallListAssignees != nil {
					t.Fatalf("%s keeps part of its list outcome: %+v", item.ID, item)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("lists = %v, want %v", got, tc.want)
			}
		})
	}
}
