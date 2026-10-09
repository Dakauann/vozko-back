package lead

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/customfield"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
)

func TestTimelineQueryNormalize(t *testing.T) {
	at := time.Date(2026, 10, 8, 9, 30, 0, 0, time.UTC)
	cursor := shared.Keyset{At: at, ID: "deal:6f1c2d3e-4b5a-4c6d-8e7f-9a0b1c2d3e4f"}.Encode()
	cases := []struct {
		name   string
		in     PageQuery
		want   PageQuery
		before *shared.Keyset
		err    error
	}{
		{"the first page gets the default size", PageQuery{LeadID: " l-1 "}, PageQuery{LeadID: "l-1", Limit: DefaultTimelinePage}, nil, nil},
		{"a large page is capped", PageQuery{LeadID: "l-1", Limit: 5000}, PageQuery{LeadID: "l-1", Limit: MaxTimelinePage}, nil, nil},
		{"the next page keeps its cursor", PageQuery{LeadID: "l-1", Before: cursor, Limit: 10}, PageQuery{LeadID: "l-1", Before: cursor, Limit: 10}, &shared.Keyset{At: at, ID: "deal:6f1c2d3e-4b5a-4c6d-8e7f-9a0b1c2d3e4f"}, nil},
		{"no lead", PageQuery{}, PageQuery{}, nil, ErrLeadRequired},
		{"a negative size", PageQuery{LeadID: "l-1", Limit: -1}, PageQuery{}, nil, ErrPageQueryInvalid},
		{"a cursor that was not ours", PageQuery{LeadID: "l-1", Before: "abc"}, PageQuery{}, nil, ErrPageQueryInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.Normalize()
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err != nil {
				return
			}
			if got != tc.want {
				t.Fatalf("query = %+v, want %+v", got, tc.want)
			}
			before, err := got.Cursor()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, tc.before) {
				t.Fatalf("cursor = %+v, want %+v", before, tc.before)
			}
		})
	}
}

func TestTimelineGrantsShowOnlyWhatTheViewerMayOpen(t *testing.T) {
	open := shared.EntryRef{EntryID: "e-open", EntryType: shared.EntryTypeWhatsApp}
	hidden := shared.EntryRef{EntryID: "e-hidden", EntryType: shared.EntryTypeInstagram}
	grants := TimelineGrants{
		Entries: map[shared.EntryRef]bool{open: true},
		Calls:   map[string]bool{"call-mine": true},
		Deals:   true,
	}
	entry := func(kind TimelineKind, ref shared.EntryRef) TimelineItem {
		return TimelineItem{Kind: kind, Ref: TimelineRef{Type: TimelineRefEntry, ID: ref.EntryID, EntryType: ref.EntryType}}
	}
	cases := []struct {
		name   string
		item   TimelineItem
		grants TimelineGrants
		want   bool
	}{
		{"a conversation the viewer can open", entry(TimelineConversation, open), grants, true},
		{"a conversation of another department", entry(TimelineConversation, hidden), grants, false},
		{"a campaign read of an open conversation", entry(TimelineCampaignRead, open), grants, true},
		{"a campaign send whose conversation is hidden", entry(TimelineCampaignSent, hidden), grants, false},
		{"a campaign send with no conversation yet", TimelineItem{Kind: TimelineCampaignSent, Ref: TimelineRef{Type: TimelineRefEntry, EntryType: shared.EntryTypeUnofficialWhatsApp}}, grants, false},
		{"an entry item with an unknown channel", entry(TimelineConversation, shared.EntryRef{EntryID: "e-open", EntryType: "fax"}), grants, false},
		{"a call the viewer took part in", TimelineItem{Kind: TimelineCall, Ref: TimelineRef{Type: TimelineRefCall, ID: "call-mine"}}, grants, true},
		{"a colleague's call", TimelineItem{Kind: TimelineCall, Ref: TimelineRef{Type: TimelineRefCall, ID: "call-theirs"}}, grants, false},
		{"a call item pointing at a conversation", TimelineItem{Kind: TimelineCall, Ref: TimelineRef{Type: TimelineRefEntry, ID: "call-mine", EntryType: shared.EntryTypeWhatsApp}}, grants, false},
		{"a deal inside the viewer's scope", TimelineItem{Kind: TimelineDeal, Ref: TimelineRef{Type: TimelineRefDeal, ID: "d-1"}}, grants, true},
		{"a deal for a viewer without deal access", TimelineItem{Kind: TimelineDeal, Ref: TimelineRef{Type: TimelineRefDeal, ID: "d-1"}}, TimelineGrants{}, false},
		{"a stage move of a deal inside the viewer's scope", TimelineItem{Kind: TimelineDealEvent, Ref: TimelineRef{Type: TimelineRefDeal, ID: "d-1"}}, grants, true},
		{"a stage move for a viewer without deal access", TimelineItem{Kind: TimelineDealEvent, Ref: TimelineRef{Type: TimelineRefDeal, ID: "d-1"}}, TimelineGrants{}, false},
		{"a stage move pointing at a conversation", TimelineItem{Kind: TimelineDealEvent, Ref: TimelineRef{Type: TimelineRefEntry, ID: "e-open", EntryType: shared.EntryTypeWhatsApp}}, grants, false},
		{"a memory", TimelineItem{Kind: TimelineMemory, Ref: TimelineRef{Type: TimelineRefMemory, ID: "m-1"}}, TimelineGrants{}, true},
		{"a memory item pointing elsewhere", TimelineItem{Kind: TimelineMemory, Ref: TimelineRef{Type: TimelineRefDeal, ID: "m-1"}}, grants, false},
		{"a record event", TimelineItem{Kind: TimelineRecord, Ref: TimelineRef{Type: TimelineRefEvent, ID: "ev-1"}}, TimelineGrants{}, true},
		{"a record item pointing elsewhere", TimelineItem{Kind: TimelineRecord, Ref: TimelineRef{Type: TimelineRefMemory, ID: "ev-1"}}, grants, false},
		{"a kind this server does not know", TimelineItem{Kind: "sms", Ref: TimelineRef{Type: TimelineRefEntry, ID: "e-open", EntryType: shared.EntryTypeWhatsApp}}, grants, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, got := tc.grants.Visible(tc.item); got != tc.want {
				t.Fatalf("Visible = %v, want %v", got, tc.want)
			}
		})
	}
}

func sensitiveFieldViewer(readsSensitive bool) Viewer {
	return Viewer{
		ReadsLeads: true,
		Fields:     customfield.Viewer{ReadsSensitive: readsSensitive},
		Definitions: []*customfield.Definition{
			{Key: "curso", Type: customfield.TypeText},
			{Key: "partido", Type: customfield.TypeText, Sensitive: true},
		},
	}
}

func TestOnlyTheChangedFieldsTheViewerMayReadAreNamed(t *testing.T) {
	changed := []string{FieldName, CustomFieldName("curso"), CustomFieldName("partido"), CustomFieldName("apagado")}
	cases := []struct {
		name   string
		fields []string
		viewer Viewer
		want   []string
	}{
		{"a viewer without read_sensitive", changed, sensitiveFieldViewer(false), []string{FieldName, CustomFieldName("curso")}},
		{"a viewer with read_sensitive", changed, sensitiveFieldViewer(true), []string{FieldName, CustomFieldName("curso"), CustomFieldName("partido")}},
		{"no definitions hide every custom field", changed, Viewer{Fields: customfield.Viewer{ReadsSensitive: true}}, []string{FieldName}},
		{"nothing changed", nil, sensitiveFieldViewer(false), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := VisibleChangedFields(tc.fields, tc.viewer); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("fields = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestARecordEventNamesOnlyTheFieldsTheViewerMayRead(t *testing.T) {
	record := func(fields ...string) TimelineItem {
		return TimelineItem{Kind: TimelineRecord, Ref: TimelineRef{Type: TimelineRefEvent, ID: "ev-1"}, Summary: TimelineSummary{Event: "updated", Fields: fields}}
	}
	cases := []struct {
		name    string
		item    TimelineItem
		viewer  Viewer
		visible bool
		fields  []string
	}{
		{"a sensitive key is never named", record(FieldName, CustomFieldName("partido")), sensitiveFieldViewer(false), true, []string{FieldName}},
		{"a change of only sensitive fields is left out", record(CustomFieldName("partido")), sensitiveFieldViewer(false), false, nil},
		{"a viewer who reads sensitive fields sees the key", record(CustomFieldName("partido")), sensitiveFieldViewer(true), true, []string{CustomFieldName("partido")}},
		{"an event that changed no field stays", record(), sensitiveFieldViewer(false), true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, visible := TimelineGrants{Fields: tc.viewer}.Visible(tc.item)
			if visible != tc.visible {
				t.Fatalf("visible = %v, want %v", visible, tc.visible)
			}
			if visible && !reflect.DeepEqual(got.Summary.Fields, tc.fields) {
				t.Fatalf("fields = %v, want %v", got.Summary.Fields, tc.fields)
			}
		})
	}
}

func TestTimelineEntriesAreTheConversationsTheItemsPointAt(t *testing.T) {
	items := []TimelineItem{
		{Kind: TimelineConversation, Ref: TimelineRef{Type: TimelineRefEntry, ID: "e-1", EntryType: shared.EntryTypeWhatsApp}},
		{Kind: TimelineCampaignSent, Ref: TimelineRef{Type: TimelineRefEntry, ID: "e-1", EntryType: shared.EntryTypeWhatsApp}},
		{Kind: TimelineCampaignSent, Ref: TimelineRef{Type: TimelineRefEntry, EntryType: shared.EntryTypeUnofficialWhatsApp}},
		{Kind: TimelineCall, Ref: TimelineRef{Type: TimelineRefCall, ID: "c-1"}},
		{Kind: TimelineConversation, Ref: TimelineRef{Type: TimelineRefEntry, ID: "e-2", EntryType: shared.EntryTypeTelegram}},
	}
	want := []shared.EntryRef{{EntryID: "e-1", EntryType: shared.EntryTypeWhatsApp}, {EntryID: "e-2", EntryType: shared.EntryTypeTelegram}}
	if got := TimelineEntries(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %+v, want %+v", got, want)
	}
	if got := TimelineCalls(items); !reflect.DeepEqual(got, []string{"c-1"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestOnlyTheLeadsOwnNumberFindsItsOlderCalls(t *testing.T) {
	l := &Lead{Number: "5511987654321", Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}}
	want := []string{"5511987654321", "551187654321", "+5511987654321", "+551187654321"}
	if got := l.LegacyCallForms(); !reflect.DeepEqual(got, want) {
		t.Fatalf("forms = %v, want %v: a contact phone may be a family landline other leads share", got, want)
	}
	if got := (&Lead{Phones: []ContactPhone{{Number: "551133334444", Label: PhoneLandline}}}).LegacyCallForms(); len(got) != 0 {
		t.Fatalf("a lead without its own number has forms %v", got)
	}
}

func TestNumberFormsSplitTheOwnNumberFromContactPhones(t *testing.T) {
	masks := []NumberMask{{Forms: []string{"a", "+a"}, Identity: true}, {Forms: []string{"b", "+b"}}, {Forms: []string{"c"}}}
	if got := NumberForms(masks, true); !reflect.DeepEqual(got, []string{"a", "+a"}) {
		t.Fatalf("own forms = %v", got)
	}
	if got := NumberForms(masks, false); !reflect.DeepEqual(got, []string{"b", "+b", "c"}) {
		t.Fatalf("contact forms = %v", got)
	}
	if got := ContactForms(masks); !reflect.DeepEqual(got, NumberForms(masks, false)) {
		t.Fatalf("ContactForms = %v", got)
	}
}

type namesStub map[string]string

func (n namesStub) Names(ids ...string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := n[id]; ok {
			out[id] = name
		}
	}
	return out
}

func TestTimelineActorsAreNamedInOneLookup(t *testing.T) {
	items := []TimelineItem{{Actor: "u-1"}, {Actor: "ai:a-1"}, {Actor: ""}, {Actor: "u-1"}}
	NameTimelineActors(namesStub{"u-1": "Maria", "ai:a-1": "Elo"}, items)
	got := []string{items[0].ActorName, items[1].ActorName, items[2].ActorName, items[3].ActorName}
	if !reflect.DeepEqual(got, []string{"Maria", "Elo", "", "Maria"}) {
		t.Fatalf("names = %v", got)
	}
	NameTimelineActors(nil, items)
}

func TestATimelineItemPagesByItsTimeAndID(t *testing.T) {
	at := time.Date(2026, 10, 8, 9, 30, 0, 0, time.UTC)
	item := TimelineItem{ID: "call:1", At: at}
	if got := item.Cursor(); got != (shared.Keyset{At: at, ID: "call:1"}) {
		t.Fatalf("cursor = %+v", got)
	}
}

func TestABrokenPageQueryHasItsOwnInputCode(t *testing.T) {
	if got := ErrorCode(ErrPageQueryInvalid); got != "lead_page_invalid" {
		t.Fatalf("code = %q", got)
	}
	if !IsInputRefusal(ErrPageQueryInvalid) {
		t.Fatal("a broken page query is the caller's input")
	}
}

func TestADealsCursorPointsAtADeal(t *testing.T) {
	at := time.Date(2026, 10, 8, 9, 30, 0, 0, time.UTC)
	deal := "6f1c2d3e-4b5a-4c6d-8e7f-9a0b1c2d3e4f"
	cases := []struct {
		name   string
		before string
		want   *shared.Keyset
		err    error
	}{
		{"the first page has no cursor", "", nil, nil},
		{"a deals cursor", shared.Keyset{At: at, ID: deal}.Encode(), &shared.Keyset{At: at, ID: deal}, nil},
		{"a timeline cursor is not a deals cursor", shared.Keyset{At: at, ID: "deal:" + deal}.Encode(), nil, ErrPageQueryInvalid},
		{"a cursor that was not ours", "abc", nil, ErrPageQueryInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PageQuery{LeadID: "l-1", Before: tc.before}.DealsCursor()
			if !errors.Is(err, tc.err) || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("DealsCursor() = %+v, %v; want %+v, %v", got, err, tc.want, tc.err)
			}
		})
	}
}

func TestTheTimelineShowsADealsMovesThroughItsStagesAndItsOutcome(t *testing.T) {
	shown := map[opportunity.EventType]bool{}
	for _, event := range TimelineDealEvents() {
		shown[event] = true
	}
	cases := []struct {
		event opportunity.EventType
		want  bool
	}{
		{opportunity.EventStageMoved, true},
		{opportunity.EventWon, true},
		{opportunity.EventLost, true},
		{opportunity.EventReopened, true},
		{opportunity.EventCreated, false},
		{opportunity.EventValueChanged, false},
		{opportunity.EventOwnerChanged, false},
		{opportunity.EventLinked, false},
	}
	for _, tc := range cases {
		t.Run(string(tc.event), func(t *testing.T) {
			if shown[tc.event] != tc.want {
				t.Fatalf("%s shown = %v, want %v", tc.event, shown[tc.event], tc.want)
			}
		})
	}
	if len(shown) != len(TimelineDealEvents()) {
		t.Fatalf("an event is listed twice: %v", TimelineDealEvents())
	}
}

func listedCall(listID string, assignees ...string) TimelineItem {
	callback := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	return TimelineItem{
		Kind: TimelineCall, Ref: TimelineRef{Type: TimelineRefCall, ID: "call-mine"},
		Summary:           TimelineSummary{Direction: "outbound", Disposition: "_callback", CallbackAt: &callback, CallListID: listID},
		CallListAssignees: assignees,
	}
}

func TestACallNamesItsListOutcomeOnlyToWhoMaySeeThatList(t *testing.T) {
	callback := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	withOutcome := TimelineSummary{Direction: "outbound", Disposition: "_callback", CallbackAt: &callback, CallListID: "list-1"}
	bare := TimelineSummary{Direction: "outbound"}
	cases := []struct {
		name   string
		grants TimelineGrants
		item   TimelineItem
		want   TimelineSummary
	}{
		{"a viewer of the list", TimelineGrants{Calls: map[string]bool{"call-mine": true}, CallLists: map[string]bool{"list-1": true}}, listedCall("list-1", "u-1"), withOutcome},
		{"a viewer of the call outside the list", TimelineGrants{Calls: map[string]bool{"call-mine": true}}, listedCall("list-1", "u-1"), bare},
		{"a viewer of another list", TimelineGrants{Calls: map[string]bool{"call-mine": true}, CallLists: map[string]bool{"list-2": true}}, listedCall("list-1"), bare},
		{"a call from no list", TimelineGrants{Calls: map[string]bool{"call-mine": true}, CallLists: map[string]bool{"": true}}, listedCall(""), bare},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, shown := tc.grants.Visible(tc.item)
			if !shown {
				t.Fatal("a call the viewer may open stays on the timeline")
			}
			if !reflect.DeepEqual(got.Summary, tc.want) {
				t.Fatalf("summary = %+v, want %+v", got.Summary, tc.want)
			}
			if got.CallListAssignees != nil {
				t.Fatalf("who works the list never leaves the rule: %v", got.CallListAssignees)
			}
		})
	}
}

func TestTimelineCallListsAreTheListsTheCallsWereWorkedOn(t *testing.T) {
	items := []TimelineItem{
		listedCall("list-1", "u-1", "u-2"),
		listedCall("list-1", "u-1", "u-2"),
		listedCall("list-2"),
		listedCall(""),
		{Kind: TimelineDeal, Ref: TimelineRef{Type: TimelineRefDeal, ID: "d-1"}, Summary: TimelineSummary{CallListID: "list-9"}},
	}
	want := map[string][]string{"list-1": {"u-1", "u-2"}, "list-2": nil}
	if got := TimelineCallLists(items); !reflect.DeepEqual(got, want) {
		t.Fatalf("call lists = %v, want %v", got, want)
	}
}
