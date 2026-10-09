package lead

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"vozko/domain/lead"
	"vozko/domain/opportunity"
	"vozko/domain/shared"
	"vozko/infra/database"
	"vozko/infra/database/schema"
	opportunity_repository "vozko/infra/repositories/opportunity"
)

type timelineWorld struct {
	db       *gorm.DB
	repo     *repository
	ws       string
	maria    *lead.Lead
	ids      map[string]string
	times    map[string]time.Time
	expected []string
	deals    map[string]string
}

func newTimelineWorld(t *testing.T) timelineWorld {
	t.Helper()
	db, repo := collectionsDB(t)
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(&schema.LeadMemory{}, &schema.UnofficialWhatsAppCampaignEntry{}, &schema.WhatsAppCampaign{}, &schema.UnofficialWhatsAppCampaign{},
		&schema.Call{}, &schema.Opportunity{}, &schema.OpportunityConversation{}, &schema.ConversationMessage{},
		&schema.OpportunityEvent{}, &schema.Stage{}, &schema.CallList{}, &schema.CallListItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	ws, otherWS := uuid.NewString(), uuid.NewString()
	w := timelineWorld{db: db, repo: repo, ws: ws, ids: map[string]string{}, times: map[string]time.Time{}, deals: map[string]string{}}
	w.maria = newLead(t, repo, ws, lead.Draft{Number: "5511987654321", Name: "Maria Souza", Phones: []lead.ContactPhone{{Number: "1133334444", Label: lead.PhoneLandline}}})
	joao := newLead(t, repo, ws, lead.Draft{Number: "5511912345678", Name: "João"})
	t0 := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	at := func(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }
	message := func(entryID string, entryType shared.EntryType, kind, metadata string, created time.Time) {
		var meta []byte
		if metadata != "" {
			meta = []byte(metadata)
		}
		must(db.Create(&schema.ConversationMessage{ID: uuid.NewString(), EntryID: entryID, EntryType: string(entryType), MessageType: kind, Metadata: meta, CreatedAt: created}).Error)
	}

	campaign, paused := uuid.NewString(), uuid.NewString()
	must(db.Create(&schema.WhatsAppCampaign{ID: campaign, WorkspaceID: ws, Name: "Volta às aulas"}).Error)
	must(db.Create(&schema.WhatsAppCampaign{ID: paused, WorkspaceID: ws, Name: "Disparo pausado"}).Error)
	entry := uuid.NewString()
	sent, read := at(2), at(3)
	must(db.Create(&schema.WhatsAppCampaignEntry{ID: entry, CampaignID: campaign, LeadID: w.maria.ID, Status: "READ", CreatedAt: at(1), SentAt: &sent, ReadAt: &read}).Error)
	w.times["official"] = sent.Add(-time.Second)
	message(entry, shared.EntryTypeWhatsApp, "template", "", w.times["official"])
	must(db.Create(&schema.WhatsAppCampaignEntry{ID: uuid.NewString(), CampaignID: campaign, LeadID: joao.ID, CreatedAt: at(1), SentAt: &sent}).Error)
	w.ids["pending"] = uuid.NewString()
	must(db.Create(&schema.WhatsAppCampaignEntry{ID: w.ids["pending"], CampaignID: paused, LeadID: w.maria.ID, Status: "PENDING", CreatedAt: at(1)}).Error)

	contact, conversation := uuid.NewString(), uuid.NewString()
	must(db.Create(&schema.TelegramContact{ID: contact, WorkspaceID: ws, AccountID: uuid.NewString(), TGUserID: 7, TGChatID: 7, LeadID: &w.maria.ID}).Error)
	must(db.Create(&schema.TelegramConversation{ID: conversation, WorkspaceID: ws, AccountID: uuid.NewString(), ContactID: contact, TGChatID: 7, CreatedAt: at(4)}).Error)
	w.times["telegram"] = at(4).Add(30 * time.Second)
	message(conversation, shared.EntryTypeTelegram, "system", `{"seed":"other"}`, w.times["telegram"])
	message(conversation, shared.EntryTypeTelegram, "text", "", at(4).Add(40*time.Second))

	uwContact, seeded := uuid.NewString(), uuid.NewString()
	must(db.Create(&schema.UnofficialWhatsAppContact{ID: uwContact, WorkspaceID: ws, InstanceID: uuid.NewString(), JID: "5511987654321@s.whatsapp.net", LeadID: &w.maria.ID}).Error)
	must(db.Create(&schema.UnofficialWhatsAppConversation{ID: seeded, WorkspaceID: ws, InstanceID: uuid.NewString(), ContactID: uwContact, ChatID: "5511987654321@s.whatsapp.net", CreatedAt: at(4)}).Error)
	message(seeded, shared.EntryTypeUnofficialWhatsApp, "system", `{"seed":"lead_import"}`, at(4).Add(10*time.Second))
	w.ids["seeded"] = seeded

	unofficial, uwConversation := uuid.NewString(), uuid.NewString()
	uwSent := at(5)
	must(db.Create(&schema.UnofficialWhatsAppCampaignEntry{ID: unofficial, CampaignID: uuid.NewString(), WorkspaceID: ws, LeadID: w.maria.ID,
		Number: "5511987654321", ConversationID: &uwConversation, SentAt: &uwSent}).Error)

	call := func(workspaceID string, leadID *string, direction, from, to string, minutes int) string {
		id, agent := uuid.NewString(), uuid.NewString()
		must(db.Create(&schema.Call{ID: id, CallID: "provider-" + id, WorkspaceID: workspaceID, Type: "voice", Direction: direction, Source: "sip",
			Status: "completed", LeadID: leadID, AgentID: &agent, PhoneFrom: from, PhoneTo: to, StartedAt: at(minutes)}).Error)
		return id
	}
	linkedCall := call(ws, &w.maria.ID, "outbound", "551130000000", "5511987654321", 6)
	oldInbound := call(ws, nil, "inbound", "+5511987654321", "551130000000", 7)
	w.ids["landline call"] = call(ws, nil, "outbound", "551130000000", "551133334444", 8)
	call(otherWS, nil, "inbound", "+5511987654321", "551130000000", 9)
	call(ws, &joao.ID, "outbound", "551130000000", "5511912345678", 9)
	call(ws, nil, "outbound", "+5511987654321", "551130000000", 9)

	deal := func(workspaceID string, leadID, owner string, minutes int, deleted bool) string {
		row := &schema.Opportunity{ID: uuid.NewString(), WorkspaceID: workspaceID, LeadID: leadID, PipelineID: uuid.NewString(), StageID: uuid.NewString(),
			OwnerID: owner, OwnerKind: "human", CreatedByID: owner, CreatedByKind: "human", Title: "Matrícula", Status: "open", CreatedAt: at(minutes)}
		must(db.Create(row).Error)
		if deleted {
			must(db.Exec("UPDATE opportunities SET deleted_at = now() WHERE id = ?", row.ID).Error)
		}
		return row.ID
	}
	link := func(dealID string) {
		must(db.Create(&schema.OpportunityConversation{ID: uuid.NewString(), OpportunityID: dealID, EntryID: entry, EntryType: "whatsapp"}).Error)
	}
	seller, colleague := uuid.NewString(), uuid.NewString()
	w.deals["own"] = deal(ws, w.maria.ID, seller, 9, false)
	w.deals["linked"] = deal(ws, "", colleague, 10, false)
	link(w.deals["linked"])
	w.deals["both"] = deal(ws, w.maria.ID, seller, 11, false)
	link(w.deals["both"])
	w.deals["seller"] = seller
	deal(ws, w.maria.ID, seller, 12, true)
	deal(otherWS, w.maria.ID, seller, 12, false)
	deal(ws, joao.ID, seller, 12, false)

	memory := uuid.NewString()
	must(db.Create(&schema.LeadMemory{ID: memory, WorkspaceID: ws, LeadID: w.maria.ID, Category: "family", Content: "tem dois filhos", ActorKind: "ai", ActorID: "ai:agent-1", CreatedAt: at(13)}).Error)
	must(db.Create(&schema.LeadMemory{ID: uuid.NewString(), WorkspaceID: ws, LeadID: joao.ID, Category: "family", Content: "x", ActorKind: "human", ActorID: seller, CreatedAt: at(13)}).Error)
	event := uuid.NewString()
	must(db.Exec("INSERT INTO lead_events (id, workspace_id, lead_id, actor_kind, kind, changes, created_at) VALUES (?, ?, ?, 'system', 'updated', ?::jsonb, ?)",
		event, ws, w.maria.ID, `[{"field":"name","before":"Maria","after":"Maria Souza"},{"field":"email","before":null,"after":"maria@exemplo.com.br"}]`, at(14)).Error)

	w.ids["record"] = "record:" + event
	w.ids["memory"] = "memory:" + memory
	w.ids["own deal"] = "deal:" + w.deals["own"]
	w.ids["linked call"] = "call:" + linkedCall
	w.ids["unofficial sent"] = "campaign_sent:" + unofficial
	w.ids["telegram"] = "conversation:telegram:" + conversation
	w.ids["official read"] = "campaign_read:" + entry
	w.ids["official"] = "conversation:whatsapp:" + entry
	w.expected = []string{
		w.ids["record"],
		w.ids["memory"],
		"deal:" + w.deals["both"],
		"deal:" + w.deals["linked"],
		w.ids["own deal"],
		"call:" + oldInbound,
		w.ids["linked call"],
		w.ids["unofficial sent"],
		w.ids["telegram"],
		w.ids["official read"],
		"campaign_sent:" + entry,
		w.ids["official"],
	}
	return w
}

func (w timelineWorld) scan(t *testing.T, scan lead.TimelineScan) []lead.TimelineItem {
	t.Helper()
	scan.WorkspaceID, scan.LeadID = w.ws, w.maria.ID
	scan.NumberForms = w.maria.LegacyCallForms()
	items, err := w.repo.ScanTimeline(context.Background(), scan)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	return items
}

func timelineIDs(items []lead.TimelineItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func TestTheTimelineReadsEverySourceOfTheLeadAgainstPostgres(t *testing.T) {
	w := newTimelineWorld(t)
	items := w.scan(t, lead.TimelineScan{Limit: 50, Calls: true, Deals: &opportunity.DealScope{}})
	if got := timelineIDs(items); !reflect.DeepEqual(got, w.expected) {
		t.Fatalf("timeline =\n%v\nwant\n%v", got, w.expected)
	}
	byID := map[string]lead.TimelineItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	if record := byID[w.ids["record"]]; record.Actor != "system" || record.Summary.Event != "updated" || !reflect.DeepEqual(record.Summary.Fields, []string{"name", "email"}) {
		t.Fatalf("record item = %+v", record)
	}
	if memory := byID[w.ids["memory"]]; memory.Actor != "ai:agent-1" || memory.Summary.Text != "tem dois filhos" || memory.Ref.Type != lead.TimelineRefMemory {
		t.Fatalf("memory item = %+v", memory)
	}
	if read := byID[w.ids["official read"]]; read.Ref.EntryType != shared.EntryTypeWhatsApp || read.Summary.Title != "Volta às aulas" || read.Summary.Status != "READ" {
		t.Fatalf("campaign item = %+v", read)
	}
	if uw := byID[w.ids["unofficial sent"]]; uw.Ref.EntryType != shared.EntryTypeUnofficialWhatsApp || uw.Ref.ID == "" {
		t.Fatalf("unofficial campaign item = %+v", uw)
	}
	call := byID[w.ids["linked call"]]
	if call.Ref.Type != lead.TimelineRefCall || call.Ref.ID == "" || call.Actor == "" || call.Summary.Direction != "outbound" || call.Summary.Source != "sip" {
		t.Fatalf("call item = %+v", call)
	}
	if call.Call == nil || call.Call.CallID != call.Ref.ID || call.Call.Direction != "outbound" || call.Call.AgentID == nil || *call.Call.AgentID != call.Actor {
		t.Fatalf("call facts = %+v", call.Call)
	}
	if own := byID[w.ids["own deal"]]; own.Actor != w.deals["seller"] || own.Summary.Title != "Matrícula" || own.Summary.Status != "open" || own.Call != nil {
		t.Fatalf("deal item = %+v", own)
	}
}

func TestAConversationStartsAtItsFirstRealMessageAgainstPostgres(t *testing.T) {
	w := newTimelineWorld(t)
	items := w.scan(t, lead.TimelineScan{Limit: 50})
	byID := map[string]lead.TimelineItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	for name, want := range map[string]time.Time{"official": w.times["official"], "telegram": w.times["telegram"]} {
		if got := byID[w.ids[name]].At; !got.Equal(want) {
			t.Errorf("%s conversation starts at %v, want its first message at %v", name, got, want)
		}
	}
	for _, item := range items {
		switch {
		case item.Ref.ID == w.ids["pending"]:
			t.Fatalf("%s: an entry of a paused campaign that was never sent is not a conversation", item.ID)
		case item.Ref.ID == w.ids["seeded"]:
			t.Fatalf("%s: a chat holding only the import placeholder is not a conversation", item.ID)
		}
	}
}

func TestOlderCallsAreFoundByTheLeadsOwnNumberOnlyAgainstPostgres(t *testing.T) {
	w := newTimelineWorld(t)
	for _, item := range w.scan(t, lead.TimelineScan{Limit: 50, Calls: true}) {
		if item.ID == "call:"+w.ids["landline call"] {
			t.Fatal("a call on a contact phone other leads may share reached this lead's timeline")
		}
	}
}

func TestTheTimelinePagesThroughTheSameHistoryAgainstPostgres(t *testing.T) {
	w := newTimelineWorld(t)
	var got []string
	var before *shared.Keyset
	for page := 0; page < 10; page++ {
		items := w.scan(t, lead.TimelineScan{Limit: 4, Calls: true, Deals: &opportunity.DealScope{}, Before: before})
		if len(items) == 0 {
			break
		}
		got = append(got, timelineIDs(items)...)
		last := items[len(items)-1].Cursor()
		before = &last
	}
	if !reflect.DeepEqual(got, w.expected) {
		t.Fatalf("pages =\n%v\nwant\n%v", got, w.expected)
	}
}

func TestTheTimelineReadsOnlyTheSourcesItIsGivenAgainstPostgres(t *testing.T) {
	w := newTimelineWorld(t)
	items := w.scan(t, lead.TimelineScan{Limit: 50})
	var kept []string
	for _, item := range items {
		if item.Kind == lead.TimelineCall || item.Kind == lead.TimelineDeal {
			t.Fatalf("%s was read without the grant", item.ID)
		}
		kept = append(kept, item.ID)
	}
	var want []string
	for _, id := range w.expected {
		if !strings.HasPrefix(id, "call:") && !strings.HasPrefix(id, "deal:") {
			want = append(want, id)
		}
	}
	if !reflect.DeepEqual(kept, want) {
		t.Fatalf("items = %v, want %v", kept, want)
	}
	scoped := w.scan(t, lead.TimelineScan{Limit: 50, Deals: &opportunity.DealScope{Restrict: true, AssigneeOverride: w.deals["seller"]}})
	var deals []string
	for _, item := range scoped {
		if item.Kind == lead.TimelineDeal {
			deals = append(deals, item.Ref.ID)
		}
	}
	if !reflect.DeepEqual(deals, []string{w.deals["both"], w.deals["own"]}) {
		t.Fatalf("scoped deals = %v", deals)
	}
}

func TestTheDealsOfALeadIncludeADealWithoutAConversationAgainstPostgres(t *testing.T) {
	w := newTimelineWorld(t)
	reader := opportunity_repository.NewLeadDealReader(w.db)
	read := func(q opportunity.LeadDealsQuery) []string {
		t.Helper()
		q.WorkspaceID, q.LeadID = w.ws, w.maria.ID
		deals, err := reader.DealsOfLead(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(deals))
		for _, d := range deals {
			ids = append(ids, d.ID)
		}
		return ids
	}
	all := read(opportunity.LeadDealsQuery{Limit: 10})
	if !reflect.DeepEqual(all, []string{w.deals["both"], w.deals["linked"], w.deals["own"]}) {
		t.Fatalf("deals = %v", all)
	}
	first := read(opportunity.LeadDealsQuery{Limit: 2})
	var created time.Time
	if err := w.db.Raw("SELECT created_at FROM opportunities WHERE id = ?", first[1]).Scan(&created).Error; err != nil {
		t.Fatal(err)
	}
	rest := read(opportunity.LeadDealsQuery{Limit: 2, Before: &shared.Keyset{At: created, ID: first[1]}})
	if !reflect.DeepEqual(append(first, rest...), all) {
		t.Fatalf("pages %v then %v, want %v", first, rest, all)
	}
	if scoped := read(opportunity.LeadDealsQuery{Limit: 10, Scope: opportunity.DealScope{Restrict: true, AssigneeOverride: w.deals["seller"]}}); !reflect.DeepEqual(scoped, []string{w.deals["both"], w.deals["own"]}) {
		t.Fatalf("scoped deals = %v", scoped)
	}
	if none := read(opportunity.LeadDealsQuery{Limit: 10, Scope: opportunity.DealScope{Restrict: true}}); len(none) != 0 {
		t.Fatalf("an empty scope read %v", none)
	}
}

const unlinkedCallVolume = 200000

func TestOlderCallsAreLookedUpPerNumberOnAWorkspaceFullOfCallsAgainstPostgres(t *testing.T) {
	w := newTimelineWorld(t)
	if err := w.db.Exec(`INSERT INTO calls (id, call_id, workspace_id, type, direction, source, status, phone_from, phone_to, started_at, created_at, updated_at)
		SELECT gen_random_uuid(), 'noise-' || g, ?, 'voice', CASE WHEN g % 2 = 0 THEN 'inbound' ELSE 'outbound' END, 'sip', 'completed',
			'5511' || lpad((g * 7)::text, 9, '0'), '5511' || lpad((g * 13)::text, 9, '0'), now() - (g || ' seconds')::interval, now(), now()
		FROM generate_series(1, ?) AS g`, w.ws, unlinkedCallVolume).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{database.UnlinkedCallsByCounterpartIndex, "idx_opportunities_workspace_lead"} {
		sql, ok := database.ConcurrentIndexSQL(name)
		if !ok {
			t.Fatalf("%s is not declared", name)
		}
		if err := w.db.Exec(sql).Error; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if err := w.db.Exec("ANALYZE calls").Error; err != nil {
		t.Fatal(err)
	}
	sql, args := timelineQuery(lead.TimelineScan{WorkspaceID: w.ws, LeadID: w.maria.ID, NumberForms: w.maria.LegacyCallForms(), Limit: 31, Calls: true, Deals: &opportunity.DealScope{}})
	var plan []string
	if err := w.db.Raw("EXPLAIN (ANALYZE, BUFFERS) "+sql, args...).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	text := strings.Join(plan, "\n")
	if strings.Contains(text, "idx_calls_ws_started") {
		t.Fatalf("older calls walk every call of the workspace:\n%s", text)
	}
	if !strings.Contains(text, database.UnlinkedCallsByCounterpartIndex) {
		t.Fatalf("older calls are not read through %s:\n%s", database.UnlinkedCallsByCounterpartIndex, text)
	}
	if removed := rowsRemovedByFilter(plan); removed > 1000 {
		t.Fatalf("the plan discards %d rows to find a handful of calls:\n%s", removed, text)
	}
}

func rowsRemovedByFilter(plan []string) int {
	const marker = "Rows Removed by Filter: "
	removed := 0
	for _, line := range plan {
		if _, count, found := strings.Cut(line, marker); found {
			n, err := strconv.Atoi(strings.TrimSpace(count))
			if err == nil {
				removed += n
			}
		}
	}
	return removed
}

type timelineHistory struct {
	moved, won, colleagueMoved, interested, callback, refused, refusedCallback string
	callbackAt                                                                 time.Time
	listID                                                                     string
	assignees                                                                  []string
}

func (w timelineWorld) addDealMovesAndCallOutcomes(t *testing.T) timelineHistory {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond).Add(-30 * time.Minute)
	at := func(minutes int) time.Time { return now.Add(time.Duration(minutes) * time.Minute) }
	stage := func(name string) string {
		id := uuid.NewString()
		must(w.db.Create(&schema.Stage{ID: id, WorkspaceID: w.ws, Name: name}).Error)
		return id
	}
	novo, visita := stage("Novo contato"), stage("Visita agendada")
	event := func(workspaceID, dealID string, kind opportunity.EventType, from, to string, minutes int) string {
		id := uuid.NewString()
		must(w.db.Create(&schema.OpportunityEvent{ID: id, WorkspaceID: workspaceID, OpportunityID: dealID, Type: string(kind), ActorID: w.deals["seller"],
			ActorKind: "human", FromStageID: from, ToStageID: to, ValueCents: 150000, Currency: "BRL", CreatedAt: at(minutes)}).Error)
		return "deal_event:" + id
	}
	var deleted, joaos string
	must(w.db.Raw("SELECT id FROM opportunities WHERE workspace_id = ? AND deleted_at IS NOT NULL", w.ws).Scan(&deleted).Error)
	must(w.db.Raw("SELECT id FROM opportunities WHERE workspace_id = ? AND deleted_at IS NULL AND lead_id IS NOT NULL AND lead_id <> ?", w.ws, w.maria.ID).Scan(&joaos).Error)
	if deleted == "" || joaos == "" {
		t.Fatal("the world lost its deleted deal or João's deal")
	}
	h := timelineHistory{
		moved:          event(w.ws, w.deals["own"], opportunity.EventStageMoved, novo, visita, 1),
		won:            event(w.ws, w.deals["own"], opportunity.EventWon, "", visita, 2),
		colleagueMoved: event(w.ws, w.deals["linked"], opportunity.EventStageMoved, novo, visita, 3),
	}
	event(w.ws, w.deals["own"], opportunity.EventValueChanged, "", visita, 4)
	event(w.ws, w.deals["own"], opportunity.EventCreated, "", novo, 4)
	event(w.ws, deleted, opportunity.EventStageMoved, novo, visita, 4)
	event(w.ws, joaos, opportunity.EventStageMoved, novo, visita, 4)
	event(uuid.NewString(), w.deals["own"], opportunity.EventStageMoved, novo, visita, 4)

	list := func(assignees ...string) string {
		id := uuid.NewString()
		must(w.db.Create(&schema.CallList{ID: id, WorkspaceID: w.ws, Name: "Retorno de matrícula", CreatedBy: w.deals["seller"], AssigneeIDs: assignees,
			Status: "active", PhoneSource: "identity", CreatedAt: at(0), UpdatedAt: at(0)}).Error)
		return id
	}
	h.assignees = []string{w.deals["seller"], uuid.NewString()}
	h.listID = list(h.assignees...)
	position := 0
	call := func(minutes int) string {
		id, agent := uuid.NewString(), uuid.NewString()
		must(w.db.Create(&schema.Call{ID: id, CallID: "provider-" + id, WorkspaceID: w.ws, Type: "voice", Direction: "outbound", Source: "sip",
			Status: "completed", LeadID: &w.maria.ID, AgentID: &agent, PhoneFrom: "551130000000", PhoneTo: "5511987654321", StartedAt: at(minutes)}).Error)
		return id
	}
	item := func(listID, callID, disposition, refusal string, callbackAt *time.Time) {
		position++
		must(w.db.Create(&schema.CallListItem{ID: uuid.NewString(), WorkspaceID: w.ws, ListID: listID, LeadID: w.maria.ID, Phone: "5511987654321",
			Position: position, State: "closed", Disposition: schema.OptionalText(disposition), Refusal: schema.OptionalText(refusal), CallbackAt: callbackAt,
			LastCallID: schema.OptionalText(callID), CreatedAt: at(0), UpdatedAt: at(0)}).Error)
	}
	interested := strings.TrimPrefix(w.ids["linked call"], "call:")
	item(h.listID, interested, "interessado", "", nil)
	h.interested = w.ids["linked call"]
	h.callbackAt = at(24 * 60)
	callback := call(5)
	item(list(), callback, "_callback", "", &h.callbackAt)
	h.callback = "call:" + callback
	refused := call(6)
	item(list(), refused, "_refused", "", &h.callbackAt)
	h.refused = "call:" + refused
	refusedCallback := call(7)
	item(list(), refusedCallback, "_callback", "no_consent", &h.callbackAt)
	h.refusedCallback = "call:" + refusedCallback
	return h
}

func TestADealsStageMovesReachTheTimelineUnderTheDealScopeAgainstPostgres(t *testing.T) {
	w := newTimelineWorld(t)
	h := w.addDealMovesAndCallOutcomes(t)
	moves := func(scope opportunity.DealScope) map[string]lead.TimelineItem {
		out := map[string]lead.TimelineItem{}
		for _, item := range w.scan(t, lead.TimelineScan{Limit: 50, Deals: &scope}) {
			if item.Kind == lead.TimelineDealEvent {
				out[item.ID] = item
			}
		}
		return out
	}
	all := moves(opportunity.DealScope{})
	var ids []string
	for id := range all {
		ids = append(ids, id)
	}
	if len(all) != 3 || all[h.moved].ID == "" || all[h.won].ID == "" || all[h.colleagueMoved].ID == "" {
		t.Fatalf("stage moves = %v, want the two of Maria's own deal and the one of the linked deal", ids)
	}
	moved := all[h.moved]
	if moved.Ref != (lead.TimelineRef{Type: lead.TimelineRefDeal, ID: w.deals["own"]}) || moved.Actor != w.deals["seller"] {
		t.Fatalf("stage move = %+v", moved)
	}
	if s := moved.Summary; s.Event != "stage_moved" || s.Title != "Matrícula" || s.StageName != "Visita agendada" || s.FromStageName != "Novo contato" ||
		s.StageID == "" || s.FromStageID == "" || s.ValueCents != 150000 || s.Currency != "BRL" {
		t.Fatalf("stage move summary = %+v", s)
	}
	if won := all[h.won].Summary; won.Event != "won" || won.FromStageName != "" || won.StageName != "Visita agendada" {
		t.Fatalf("won summary = %+v", won)
	}
	scoped := moves(opportunity.DealScope{Restrict: true, AssigneeOverride: w.deals["seller"]})
	if _, leaked := scoped[h.colleagueMoved]; leaked || len(scoped) != 2 {
		t.Fatalf("a stage move of a deal outside the viewer's scope reached the timeline: %v", scoped)
	}
	for _, item := range w.scan(t, lead.TimelineScan{Limit: 50}) {
		if item.Kind == lead.TimelineDealEvent {
			t.Fatalf("%s was read without the deal grant", item.ID)
		}
	}
}

func TestACallShowsTheOutcomeOfTheCallListItemItWasStampedOnAgainstPostgres(t *testing.T) {
	w := newTimelineWorld(t)
	h := w.addDealMovesAndCallOutcomes(t)
	calls := map[string]lead.TimelineSummary{}
	assignees := map[string][]string{}
	for _, item := range w.scan(t, lead.TimelineScan{Limit: 50, Calls: true}) {
		if item.Kind == lead.TimelineCall {
			calls[item.ID] = item.Summary
			assignees[item.ID] = item.CallListAssignees
		}
	}
	if s := calls[h.interested]; s.Disposition != "interessado" || s.CallListID != h.listID || s.CallbackAt != nil {
		t.Fatalf("interested call = %+v", s)
	}
	if got := assignees[h.interested]; !reflect.DeepEqual(got, h.assignees) {
		t.Fatalf("the list's assignees = %v, want %v so the visibility rule can judge the outcome", got, h.assignees)
	}
	if s := calls[h.refusedCallback]; s.Disposition != "_callback" || s.CallbackAt != nil || s.CallListID == "" {
		t.Fatalf("the list's recheck time reached the timeline as the callback time: %+v", s)
	}
	if s := calls[h.callback]; s.Disposition != "_callback" || s.CallbackAt == nil || !s.CallbackAt.Equal(h.callbackAt) || s.CallListID == "" {
		t.Fatalf("callback call = %+v", s)
	}
	if s := calls[h.refused]; s.Disposition != "" || s.CallbackAt != nil || s.CallListID == "" {
		t.Fatalf("a refusal is not the outcome of the call before it: %+v", s)
	}
	for id, s := range calls {
		if id != h.interested && id != h.callback && id != h.refused && id != h.refusedCallback && (s.Disposition != "" || s.CallListID != "") {
			t.Fatalf("%s carries an outcome no item gave it: %+v", id, s)
		}
	}
}

const (
	otherDealEventVolume = 100000
	otherCallListItems   = 100000
	leadCallVolume       = 3000
	leadDealEventVolume  = 300
	leadCallLists        = 20
)

func explainLines(plan []string, index string) []string {
	var lines []string
	for _, line := range plan {
		if strings.Contains(line, index) {
			lines = append(lines, line)
		}
	}
	return lines
}

func planLoops(line string) int {
	_, rest, found := strings.Cut(line, "loops=")
	if !found {
		return -1
	}
	digits := strings.TrimRightFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
	n, err := strconv.Atoi(digits)
	if err != nil {
		return -1
	}
	return n
}

func removedPerLoopReading(plan []string, relation string) int {
	const marker = "Rows Removed by Filter: "
	node, most := "", 0
	for _, line := range plan {
		if strings.Contains(line, "(cost=") {
			node = line
			continue
		}
		_, count, found := strings.Cut(line, marker)
		if !found || !strings.Contains(node, relation) {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(count)); err == nil && n > most {
			most = n
		}
	}
	return most
}

func TestStageMovesAndCallOutcomesAreReadPerDealAndPerCallKeptAgainstPostgres(t *testing.T) {
	w := newTimelineWorld(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(w.db.Exec(`INSERT INTO opportunities (id, workspace_id, lead_id, pipeline_id, stage_id, owner_kind, created_by_kind, title, status, created_at, updated_at)
		SELECT gen_random_uuid(), ?, gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'human', 'human', 'Outro', 'open', now(), now()
		FROM generate_series(1, 1000)`, w.ws).Error)
	must(w.db.Exec(`INSERT INTO opportunity_events (id, workspace_id, opportunity_id, type, actor_kind, value_cents, currency, created_at)
		SELECT gen_random_uuid(), ?, d.ids[1 + g % 1000], CASE WHEN g % 3 = 0 THEN 'value_changed' ELSE 'stage_moved' END, 'human', 0, 'BRL', now() - (g || ' seconds')::interval
		FROM generate_series(1, ?) AS g CROSS JOIN (SELECT array_agg(id) AS ids FROM opportunities WHERE workspace_id = ? AND title = 'Outro') d`,
		w.ws, otherDealEventVolume, w.ws).Error)
	must(w.db.Exec(`INSERT INTO opportunity_events (id, workspace_id, opportunity_id, type, actor_kind, value_cents, currency, created_at)
		SELECT gen_random_uuid(), ?, ?, CASE WHEN g % 4 = 0 THEN 'value_changed' ELSE 'stage_moved' END, 'human', 0, 'BRL', now() - (g || ' minutes')::interval
		FROM generate_series(1, ?) AS g`, w.ws, w.deals["own"], leadDealEventVolume).Error)
	must(w.db.Exec(`INSERT INTO calls (id, call_id, workspace_id, type, direction, source, status, lead_id, phone_from, phone_to, started_at, created_at, updated_at)
		SELECT gen_random_uuid(), 'lead-' || g, ?, 'voice', 'outbound', 'sip', 'completed', ?, '551130000000', '5511987654321', now() - (g || ' minutes')::interval, now(), now()
		FROM generate_series(1, ?) AS g`, w.ws, w.maria.ID, leadCallVolume).Error)
	must(w.db.Exec(`INSERT INTO calls (id, call_id, workspace_id, type, direction, source, status, phone_from, phone_to, started_at, created_at, updated_at)
		SELECT gen_random_uuid(), 'noise-' || g, ?, 'voice', 'outbound', 'sip', 'completed', '551130000000', '5511' || lpad((g * 13)::text, 9, '0'), now() - (g || ' seconds')::interval, now(), now()
		FROM generate_series(1, ?) AS g`, w.ws, unlinkedCallVolume/2).Error)
	must(w.db.Exec(`INSERT INTO call_lists (id, workspace_id, name, created_by, assignee_ids, status, phone_source, created_at, updated_at)
		SELECT gen_random_uuid(), ?, 'Lista ' || g, gen_random_uuid(), ARRAY[gen_random_uuid()], 'active', 'identity', now(), now()
		FROM generate_series(1, 300) AS g`, w.ws).Error)
	others := make([]string, 0, otherCallListItems/300+1)
	for i := 0; i <= otherCallListItems/300; i++ {
		others = append(others, newLead(t, w.repo, w.ws, lead.Draft{Number: "55119" + fmt.Sprintf("%08d", i), Name: "Outro lead"}).ID)
	}
	must(w.db.Exec(`INSERT INTO call_list_items (id, workspace_id, list_id, lead_id, phone, position, state, created_at, updated_at)
		SELECT gen_random_uuid(), ?, l.ids[1 + g % 300], (?::uuid[])[1 + g / 300], '5511900000000', g, 'pending', now(), now()
		FROM generate_series(1, ?) AS g CROSS JOIN (SELECT array_agg(id) AS ids FROM call_lists WHERE workspace_id = ?) l`,
		w.ws, pq.Array(others), otherCallListItems, w.ws).Error)
	must(w.db.Exec(`INSERT INTO call_list_items (id, workspace_id, list_id, lead_id, phone, position, state, disposition, last_call_id, created_at, updated_at)
		SELECT gen_random_uuid(), ?, l.id, ?, '5511987654321', 1000000 + l.n, 'closed', 'interessado', c.id, now(), now()
		FROM (SELECT id, row_number() OVER (ORDER BY id) AS n FROM call_lists WHERE workspace_id = ? LIMIT ?) l
		CROSS JOIN LATERAL (SELECT id FROM calls WHERE lead_id = ? ORDER BY started_at DESC OFFSET l.n LIMIT 1) c`,
		w.ws, w.maria.ID, w.ws, leadCallLists, w.maria.ID).Error)
	for _, name := range []string{database.UnlinkedCallsByCounterpartIndex, "idx_opportunities_workspace_lead"} {
		sql, ok := database.ConcurrentIndexSQL(name)
		if !ok {
			t.Fatalf("%s is not declared", name)
		}
		must(w.db.Exec(sql).Error)
	}
	must(w.db.Exec("ANALYZE calls, opportunities, opportunity_events, call_lists, call_list_items").Error)

	scan := lead.TimelineScan{WorkspaceID: w.ws, LeadID: w.maria.ID, NumberForms: w.maria.LegacyCallForms(), Limit: 31, Calls: true, Deals: &opportunity.DealScope{}}
	sql, args := timelineQuery(scan)
	var plan []string
	must(w.db.Raw("EXPLAIN (ANALYZE, BUFFERS) "+sql, args...).Scan(&plan).Error)
	text := strings.Join(plan, "\n")
	if strings.Contains(text, "idx_opp_event_workspace") {
		t.Fatalf("stage moves walk every event of the workspace:\n%s", text)
	}
	if !strings.Contains(text, "idx_opp_event_opportunity") {
		t.Fatalf("stage moves are not read through each deal's events:\n%s", text)
	}
	outcome := explainLines(plan, "idx_call_list_items_lead")
	if len(outcome) == 0 {
		t.Fatalf("call outcomes are not read through the lead's items:\n%s", text)
	}
	for _, line := range outcome {
		if loops := planLoops(line); loops < 0 || loops > scan.Limit {
			t.Fatalf("the stamped item is read %d times, want at most once per call kept (%d):\n%s", loops, scan.Limit, text)
		}
	}
	if removed := removedPerLoopReading(plan, "call_list_items tci"); removed >= leadCallLists {
		t.Fatalf("each call's outcome discards %d items, more than the lead's own items:\n%s", removed, text)
	}
	if removed := removedPerLoopReading(plan, "opportunity_events tev"); removed > leadDealEventVolume/10 {
		t.Fatalf("each deal's stage moves discard %d events:\n%s", removed, text)
	}
	items, err := w.repo.ScanTimeline(context.Background(), scan)
	must(err)
	outcomes := 0
	for _, item := range items {
		if item.Kind == lead.TimelineCall && item.Summary.Disposition == "interessado" {
			outcomes++
		}
	}
	if outcomes == 0 {
		t.Fatal("the newest calls of the lead lost the outcomes of the lists they were worked on")
	}
}
