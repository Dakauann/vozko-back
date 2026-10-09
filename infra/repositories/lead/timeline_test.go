package lead

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"

	"vozko/domain/calls/cdr"
	"vozko/domain/lead"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
)

var timelineCursor = shared.Keyset{At: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), ID: "call:x"}

func fullScan() lead.TimelineScan {
	before := timelineCursor
	return lead.TimelineScan{
		WorkspaceID: "ws-1",
		LeadID:      "lead-1",
		NumberForms: []string{"5511987654321", "+5511987654321"},
		Before:      &before,
		Limit:       31,
		Calls:       true,
		Deals:       &opportunity.DealScope{Restrict: true, AssigneeOverride: "u-1"},
	}
}

func fullScanArgs() []interface{} {
	at, id := timelineCursor.At, timelineCursor.ID
	return []interface{}{
		"lead-1", at, id, 31,
		"lead-1", at, id, 31,
		"lead-1", "ws-1", at, id, 31,
		"ws-1", "lead-1", at, id, 31, 31,
		pq.Array([]string{"5511987654321", "+5511987654321"}), "ws-1", at, id, 31, 31,
		"ws-1", "ws-1", "lead-1", "lead-1", "u-1", at, id, 31,
		pq.Array([]string{"stage_moved", "won", "lost", "reopened"}), at, id, 31, "ws-1", "ws-1", "lead-1", "lead-1", "u-1", 31,
		"ws-1", "lead-1", at, id, 31,
		"ws-1", "lead-1", at, id, 31,
		31,
	}
}

func TestTheTimelineQueryReadsEverySourceTheViewerMayRead(t *testing.T) {
	sql, args := timelineQuery(fullScan())
	if got := countPlaceholders(sql); got != len(args) {
		t.Fatalf("%d placeholders for %d args in %s", got, len(args), sql)
	}
	if !reflect.DeepEqual(args, fullScanArgs()) {
		t.Fatalf("args = %v\nwant %v", args, fullScanArgs())
	}
	for _, source := range []string{
		") lead_entries CROSS JOIN LATERAL", "whatsapp_campaign_entries tw", "unofficial_whatsapp_campaign_entries tu",
		"calls tcl", "call_list_items tci", "opportunities td", "opportunity_events tev", "lead_memories tm", "lead_events te",
	} {
		if !strings.Contains(sql, source) {
			t.Errorf("the timeline misses %s", source)
		}
	}
	if branches := strings.Count(sql, " LIMIT ?)"); branches != 12 {
		t.Fatalf("%d limits, want one per branch, plus the inner bound of the lead's calls, of each number lookup and of each deal's events", branches)
	}
	if bounds := strings.Count(sql, ") < (?, ?)"); bounds != 9 {
		t.Fatalf("%d branches carry the keyset bound, want 9", bounds)
	}
	if !strings.HasSuffix(sql, ") timeline ORDER BY timeline.at DESC, timeline.item_id DESC LIMIT ?") {
		t.Fatalf("the merged page is not ordered newest first: %s", sql)
	}
	if !strings.Contains(sql, "td.workspace_id = ? AND td.deleted_at IS NULL AND td.id IN (SELECT od.id FROM opportunities od") || !strings.Contains(sql, "AND (td.owner_id = ?)") {
		t.Fatalf("deals are not limited to the lead and the viewer's scope: %s", sql)
	}
	if strings.Contains(sql, "changes ->") && strings.Contains(sql, "'before'") {
		t.Fatalf("record values must never leave the database: %s", sql)
	}
}

func TestAConversationStartsAtItsFirstMessageThatIsNotTheImportPlaceholder(t *testing.T) {
	sql, _ := timelineQuery(fullScan())
	want := ") lead_entries CROSS JOIN LATERAL (SELECT tcm.created_at AS started_at FROM conversation_messages tcm" +
		" WHERE tcm.entry_id = lead_entries.entry_id AND tcm.entry_type = lead_entries.entry_type AND tcm.deleted_at IS NULL" +
		" AND NOT (tcm.message_type = 'system' AND COALESCE(tcm.metadata->>'seed', '') = 'lead_import')" +
		" ORDER BY tcm.created_at LIMIT 1) tfirst WHERE lead_entries.lead_id = ? AND (tfirst.started_at, "
	if !strings.Contains(sql, want) {
		t.Fatalf("conversation branch does not start at the first real message: %s", sql)
	}
}

func TestOlderCallsAreReadOneIndexedLookupPerNumber(t *testing.T) {
	sql, _ := timelineQuery(fullScan())
	item := `('call:' || tcl.id::text) COLLATE "C"`
	want := "FROM unnest(?::text[]) AS tnum(number) CROSS JOIN LATERAL (SELECT tcl.* FROM calls tcl" +
		" WHERE tcl.workspace_id = ? AND tcl.lead_id IS NULL AND tcl.deleted_at IS NULL" +
		" AND (CASE WHEN tcl.direction = 'inbound' THEN tcl.phone_from ELSE tcl.phone_to END) = tnum.number" +
		" AND (tcl.started_at, " + item + ") < (?, ?) ORDER BY tcl.started_at DESC, " + item + " DESC LIMIT ?) tcl" +
		" WHERE TRUE ORDER BY tcl.started_at DESC, " + item + " DESC LIMIT ?)"
	if !strings.Contains(sql, want) {
		t.Fatalf("older calls are not looked up per number: %s", sql)
	}
}

func TestTheTimelineQueryLeavesOutWhatTheViewerMayNotRead(t *testing.T) {
	scan := fullScan()
	scan.Calls, scan.Deals, scan.Before = false, nil, nil
	sql, args := timelineQuery(scan)
	if got := countPlaceholders(sql); got != len(args) {
		t.Fatalf("%d placeholders for %d args", got, len(args))
	}
	for _, absent := range []string{"calls tcl", "call_list_items", "opportunities td", "opportunity_events", ") < (?, ?)"} {
		if strings.Contains(sql, absent) {
			t.Errorf("%q is read for a viewer who may not read it, or on the first page", absent)
		}
	}
	scan.Calls, scan.NumberForms = true, nil
	sql, _ = timelineQuery(scan)
	if strings.Count(sql, "calls tcl") != 1 || strings.Contains(sql, "unnest") {
		t.Fatalf("a lead without its own number only reads its linked calls: %s", sql)
	}
}

func TestTheTimelineQueryHasNoUnrestrictedDealsWhenTheScopeIsEmpty(t *testing.T) {
	scan := fullScan()
	scan.Deals = &opportunity.DealScope{Restrict: true}
	sql, args := timelineQuery(scan)
	if !strings.Contains(sql, "AND 1 = 0") || countPlaceholders(sql) != len(args) {
		t.Fatalf("an empty restricted scope must read no deal: %s", sql)
	}
}

var timelineRowColumns = []string{"kind", "at", "item_id", "ref_type", "ref_id", "entry_type", "actor_id", "actor_kind", "summary", "call_direction", "call_agent_id", "call_answered_at"}

func TestScanTimelineMapsEachRow(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := &repository{db: db}
	at := time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC)
	answered := at.Add(5 * time.Second)
	expectedArgs := make([]driver.Value, 0, len(fullScanArgs()))
	for _, arg := range fullScanArgs() {
		expectedArgs = append(expectedArgs, arg)
	}
	mock.ExpectQuery(`^SELECT kind, at, item_id, ref_type, ref_id, entry_type, actor_id, actor_kind, summary, call_direction, call_agent_id, call_answered_at FROM \(\(SELECT .*\) timeline ORDER BY timeline\.at DESC, timeline\.item_id DESC LIMIT \$54$`).
		WithArgs(expectedArgs...).
		WillReturnRows(sqlmock.NewRows(timelineRowColumns).
			AddRow("call", at, "call:c-1", "call", "provider-1", "", "u-1", "human",
				[]byte(`{"direction":"outbound","status":"completed","durationSec":42,"answeredAt":"`+answered.Format(time.RFC3339Nano)+`","source":"sip","disposition":"interessado","refusal":null,"callbackAt":null,"callListId":"list-1","callListAssignees":["u-1","u-2"]}`), "outbound", "u-1", answered).
			AddRow("deal_event", at.Add(-30*time.Second), "deal_event:ev-9", "deal", "d-1", "", "u-1", "human",
				[]byte(`{"title":"Matrícula","event":"stage_moved","valueCents":150000,"currency":"BRL","pipelineId":"p-1","stageId":"s-2","fromStageId":"s-1","stageName":"Visita agendada","fromStageName":"Novo contato"}`), nil, nil, nil).
			AddRow("record", at.Add(-time.Minute), "record:ev-1", "lead_event", "ev-1", "", "", "system", []byte(`{"event":"updated","fields":["name",null,"email"]}`), nil, nil, nil).
			AddRow("memory", at.Add(-2*time.Minute), "memory:m-1", "memory", "m-1", "", "ai:a-1", "ai", []byte(`{"category":"family","text":"tem dois filhos"}`), nil, nil, nil).
			AddRow("campaign_read", at.Add(-3*time.Minute), "campaign_read:e-1", "entry", "e-1", "whatsapp", "", "", []byte(`{"channel":"whatsapp","campaignId":"c-9","title":"Volta às aulas","status":"READ"}`), nil, nil, nil).
			AddRow("deal", at.Add(-4*time.Minute), "deal:d-1", "deal", "d-1", "", "w-1", "workflow", []byte(`{"title":"Matrícula","status":"open","valueCents":150000,"currency":"BRL","pipelineId":"p-1","stageId":"s-1"}`), nil, nil, nil))

	items, err := repo.ScanTimeline(context.Background(), fullScan())
	if err != nil {
		t.Fatal(err)
	}
	agent := "u-1"
	want := []lead.TimelineItem{
		{ID: "call:c-1", Kind: lead.TimelineCall, At: at, Actor: "u-1", Ref: lead.TimelineRef{Type: lead.TimelineRefCall, ID: "provider-1"},
			Summary:           lead.TimelineSummary{Direction: "outbound", Status: "completed", DurationSec: 42, AnsweredAt: &answered, Source: "sip", Disposition: "interessado", CallListID: "list-1"},
			Call:              &cdr.Call{CallID: "provider-1", Direction: cdr.DirectionOutbound, AgentID: &agent, AnsweredAt: &answered},
			CallListAssignees: []string{"u-1", "u-2"}},
		{ID: "deal_event:ev-9", Kind: lead.TimelineDealEvent, At: at.Add(-30 * time.Second), Actor: "u-1", Ref: lead.TimelineRef{Type: lead.TimelineRefDeal, ID: "d-1"},
			Summary: lead.TimelineSummary{Title: "Matrícula", Event: "stage_moved", ValueCents: 150000, Currency: "BRL", PipelineID: "p-1", StageID: "s-2",
				FromStageID: "s-1", StageName: "Visita agendada", FromStageName: "Novo contato"}},
		{ID: "record:ev-1", Kind: lead.TimelineRecord, At: at.Add(-time.Minute), Actor: "system", Ref: lead.TimelineRef{Type: lead.TimelineRefEvent, ID: "ev-1"},
			Summary: lead.TimelineSummary{Event: "updated", Fields: []string{"name", "email"}}},
		{ID: "memory:m-1", Kind: lead.TimelineMemory, At: at.Add(-2 * time.Minute), Actor: "ai:a-1", Ref: lead.TimelineRef{Type: lead.TimelineRefMemory, ID: "m-1"},
			Summary: lead.TimelineSummary{Category: "family", Text: "tem dois filhos"}},
		{ID: "campaign_read:e-1", Kind: lead.TimelineCampaignRead, At: at.Add(-3 * time.Minute), Ref: lead.TimelineRef{Type: lead.TimelineRefEntry, ID: "e-1", EntryType: shared.EntryTypeWhatsApp},
			Summary: lead.TimelineSummary{Channel: shared.EntryTypeWhatsApp, CampaignID: "c-9", Title: "Volta às aulas", Status: "READ"}},
		{ID: "deal:d-1", Kind: lead.TimelineDeal, At: at.Add(-4 * time.Minute), Actor: "workflow:w-1", Ref: lead.TimelineRef{Type: lead.TimelineRefDeal, ID: "d-1"},
			Summary: lead.TimelineSummary{Title: "Matrícula", Status: "open", ValueCents: 150000, Currency: "BRL", PipelineID: "p-1", StageID: "s-1"}},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items", len(items))
	}
	for i := range want {
		if !reflect.DeepEqual(items[i], want[i]) {
			t.Errorf("item %d = %+v\nwant %+v", i, items[i], want[i])
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestACallRowWithoutItsFactsCarriesNoCall(t *testing.T) {
	row := timelineRow{Kind: string(lead.TimelineCall), RefID: "provider-1"}
	if row.call() != nil {
		t.Fatal("a call row without its direction must not invent the facts the visibility rule reads")
	}
	direction := "inbound"
	row.CallDirection = &direction
	if call := row.call(); call == nil || call.AgentID != nil || call.Direction != cdr.DirectionInbound {
		t.Fatalf("call = %+v", call)
	}
	row.Kind = string(lead.TimelineDeal)
	if row.call() != nil {
		t.Fatal("only call rows carry call facts")
	}
}

func TestScanTimelineRefusesWithoutQuerying(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	repo := &repository{db: db}
	for name, scan := range map[string]lead.TimelineScan{
		"no workspace": {LeadID: "lead-1", Limit: 10},
		"no lead":      {WorkspaceID: "ws-1", Limit: 10},
		"no limit":     {WorkspaceID: "ws-1", LeadID: "lead-1"},
	} {
		if _, err := repo.ScanTimeline(context.Background(), scan); !errors.Is(err, errTimelineScanInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnreadableSummaryFailsTheScan(t *testing.T) {
	db, mock, sqlDB := newMockDB(t)
	defer sqlDB.Close()
	mock.ExpectQuery(`timeline`).WillReturnRows(sqlmock.NewRows(timelineRowColumns).
		AddRow("memory", time.Now(), "memory:m-1", "memory", "m-1", "", "", "", []byte(`{"text":`), nil, nil, nil))
	if _, err := (&repository{db: db}).ScanTimeline(context.Background(), fullScan()); err == nil {
		t.Fatal("a broken summary must fail the read")
	}
}

func TestACallCarriesTheOutcomeOfTheCallListItemItWasStampedOn(t *testing.T) {
	sql, _ := timelineQuery(fullScan())
	outcome := " LEFT JOIN LATERAL (SELECT tci.disposition, tci.refusal, tci.callback_at, tci.list_id," +
		" (SELECT tlst.assignee_ids FROM call_lists tlst WHERE tlst.id = tci.list_id AND tlst.workspace_id = tci.workspace_id) AS assignee_ids" +
		" FROM call_list_items tci WHERE tci.workspace_id = tcl.workspace_id AND tci.lead_id = tcl.lead_id AND tci.last_call_id = tcl.id" +
		" ORDER BY tci.updated_at DESC LIMIT 1) tco ON TRUE"
	item := `('call:' || tcl.id::text) COLLATE "C"`
	linked := "FROM (SELECT tcl.* FROM calls tcl WHERE tcl.workspace_id = ? AND tcl.deleted_at IS NULL AND tcl.lead_id = ?" +
		" AND (tcl.started_at, " + item + ") < (?, ?) ORDER BY tcl.started_at DESC, " + item + " DESC LIMIT ?) tcl" + outcome +
		" WHERE TRUE ORDER BY tcl.started_at DESC, " + item + " DESC LIMIT ?)"
	if !strings.Contains(sql, linked) {
		t.Fatalf("the stamped item is not read once per call kept on the page: %s", sql)
	}
	if got := strings.Count(sql, "call_list_items"); got != 1 {
		t.Fatalf("%d branches read call list items, want only the lead's own calls (a call with no lead was never on a list): %s", got, sql)
	}
	if !strings.Contains(sql, "'disposition', tco.disposition, 'refusal', tco.refusal, 'callbackAt', tco.callback_at, 'callListId', tco.list_id::text, 'callListAssignees', tco.assignee_ids") {
		t.Fatalf("the call summary does not carry the stamped item's outcome: %s", sql)
	}
}

func TestADealsStageMovesAreReadUnderTheSameDealScope(t *testing.T) {
	sql, _ := timelineQuery(fullScan())
	item := `('deal_event:' || tev.id::text) COLLATE "C"`
	want := "FROM opportunities td CROSS JOIN LATERAL (SELECT tev.* FROM opportunity_events tev" +
		" WHERE tev.opportunity_id = td.id AND tev.workspace_id = td.workspace_id AND tev.type = ANY(?)" +
		" AND (tev.created_at, " + item + ") < (?, ?) ORDER BY tev.created_at DESC, " + item + " DESC LIMIT ?) tev" +
		" LEFT JOIN stages tst ON tst.id = tev.to_stage_id::text AND tst.workspace_id = tev.workspace_id" +
		" LEFT JOIN stages tsf ON tsf.id = tev.from_stage_id::text AND tsf.workspace_id = tev.workspace_id" +
		" WHERE td.workspace_id = ? AND td.deleted_at IS NULL AND td.id IN (SELECT od.id FROM opportunities od"
	if !strings.Contains(sql, want) {
		t.Fatalf("stage moves are not read one deal of the lead at a time: %s", sql)
	}
	if !strings.Contains(sql, "AND (td.owner_id = ?) ORDER BY tev.created_at DESC, "+item+" DESC LIMIT ?)") {
		t.Fatalf("stage moves skip the viewer's deal scope: %s", sql)
	}
	if strings.Contains(sql, "tev.details") {
		t.Fatalf("event details never reach the timeline: %s", sql)
	}
}

func TestTheCallOutcomeOfASummaryFollowsTheCallListRule(t *testing.T) {
	callback := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		row  timelineSummaryRow
		want lead.TimelineSummary
	}{
		{"a closed item", timelineSummaryRow{Disposition: "interessado", CallListID: "list-1"}, lead.TimelineSummary{Disposition: "interessado", CallListID: "list-1"}},
		{"a callback", timelineSummaryRow{Disposition: "_callback", CallbackAt: &callback, CallListID: "list-1"},
			lead.TimelineSummary{Disposition: "_callback", CallbackAt: &callback, CallListID: "list-1"}},
		{"a refused item", timelineSummaryRow{Disposition: "_refused", CallbackAt: &callback, CallListID: "list-1"}, lead.TimelineSummary{CallListID: "list-1"}},
		{"an item still in the call", timelineSummaryRow{CallListID: "list-1"}, lead.TimelineSummary{CallListID: "list-1"}},
		{"a recheck time without a callback", timelineSummaryRow{Disposition: "interessado", CallbackAt: &callback}, lead.TimelineSummary{Disposition: "interessado"}},
		{"a callback the list later refused for a while", timelineSummaryRow{Disposition: "_callback", Refusal: "no_consent", CallbackAt: &callback, CallListID: "list-1"},
			lead.TimelineSummary{Disposition: "_callback", CallListID: "list-1"}},
		{"a call from no list", timelineSummaryRow{}, lead.TimelineSummary{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.row.summary(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("summary = %+v, want %+v", got, tc.want)
			}
		})
	}
}
