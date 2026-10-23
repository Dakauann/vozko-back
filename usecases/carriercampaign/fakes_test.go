package carriercampaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/selection"
	"vozko/domain/shared"
	"vozko/domain/sms"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/usecases/campaignqueue"
	carriermessaging_usecase "vozko/usecases/carriermessaging"
)

var errBoom = errors.New("boom")

type fakeCampaigns struct {
	mu           sync.Mutex
	byID         map[string]cc.Campaign
	beforeUpdate func()
}

func newFakeCampaigns() *fakeCampaigns { return &fakeCampaigns{byID: map[string]cc.Campaign{}} }

func (f *fakeCampaigns) put(c cc.Campaign) { f.mu.Lock(); f.byID[c.ID] = c; f.mu.Unlock() }

func (f *fakeCampaigns) get(id string) cc.Campaign {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byID[id]
}

func (f *fakeCampaigns) ClaimScheduledStart(id string, dueBy time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok || c.ScheduledAt == nil || c.ScheduledAt.After(dueBy) {
		return false, nil
	}
	c.ScheduledAt = nil
	f.byID[id] = c
	return true, nil
}

func (f *fakeCampaigns) Create(_ context.Context, c *cc.Campaign) error { f.put(*c); return nil }

func (f *fakeCampaigns) Update(_ context.Context, c *cc.Campaign, from cc.Status) error {
	if f.beforeUpdate != nil {
		f.beforeUpdate()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.byID[c.ID]
	if !ok || stored.WorkspaceID != c.WorkspaceID {
		return cc.ErrCampaignNotFound
	}
	if stored.Status != from || !from.Editable() || !c.Status.Editable() {
		return cc.ErrNotEditable
	}
	f.byID[c.ID] = *c
	return nil
}

func (f *fakeCampaigns) Delete(_ context.Context, ws, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.byID[id]; !ok || c.WorkspaceID != ws {
		return cc.ErrCampaignNotFound
	}
	delete(f.byID, id)
	return nil
}

func (f *fakeCampaigns) FindByID(_ context.Context, ws, id string) (*cc.Campaign, error) {
	c, ok := f.find(id)
	if !ok || c.WorkspaceID != ws {
		return nil, cc.ErrCampaignNotFound
	}
	return &c, nil
}

func (f *fakeCampaigns) find(id string) (cc.Campaign, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	return c, ok
}

func (f *fakeCampaigns) FindByIDUnscoped(_ context.Context, id string) (*cc.Campaign, error) {
	c, ok := f.find(id)
	if !ok {
		return nil, cc.ErrCampaignNotFound
	}
	return &c, nil
}

func (f *fakeCampaigns) List(_ context.Context, in cc.ListCampaignsInput) (*shared.PaginatedResult[*cc.Campaign], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*cc.Campaign
	for _, c := range f.byID {
		if c.WorkspaceID == in.WorkspaceID && (in.Channel == "" || c.Channel == in.Channel) {
			copied := c
			out = append(out, &copied)
		}
	}
	return shared.NewPaginatedResult(out, in.Pagination, int64(len(out))), nil
}

func (f *fakeCampaigns) ListByStatus(_ context.Context, statuses ...cc.Status) ([]*cc.Campaign, error) {
	return nil, nil
}

func (f *fakeCampaigns) ListScheduledDue(_ context.Context, now time.Time, limit int) ([]*cc.Campaign, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*cc.Campaign
	for _, c := range f.byID {
		if c.Status == cc.StatusScheduled && c.ScheduledAt != nil && !c.ScheduledAt.After(now) {
			copied := c
			out = append(out, &copied)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeCampaigns) SwapStatus(_ context.Context, id string, from, to cc.Status) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok || c.Status != from {
		return false, nil
	}
	c.Status = to
	f.byID[id] = c
	return true, nil
}

func (f *fakeCampaigns) Reschedule(_ context.Context, id string, from, to cc.Status, at *time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok || c.Status != from {
		return false, nil
	}
	c.Status, c.ScheduledAt = to, at
	f.byID[id] = c
	return true, nil
}

func (f *fakeCampaigns) CountByStatus(_ context.Context, ws string) (map[cc.Status]int64, error) {
	return nil, nil
}

type fakeRuns struct {
	mu   sync.Mutex
	byID map[string]cc.Run
}

func newFakeRuns() *fakeRuns { return &fakeRuns{byID: map[string]cc.Run{}} }

func (f *fakeRuns) put(r cc.Run) { f.mu.Lock(); f.byID[r.ID] = r; f.mu.Unlock() }

func (f *fakeRuns) get(id string) cc.Run { f.mu.Lock(); defer f.mu.Unlock(); return f.byID[id] }

func (f *fakeRuns) all() []cc.Run {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []cc.Run
	for _, r := range f.byID {
		out = append(out, r)
	}
	return out
}

func (f *fakeRuns) Create(_ context.Context, r *cc.Run) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.byID {
		if existing.CampaignID == r.CampaignID && existing.Occurrence.Equal(r.Occurrence) {
			return false, nil
		}
	}
	f.byID[r.ID] = *r
	return true, nil
}

func (f *fakeRuns) FindByID(_ context.Context, id string) (*cc.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return nil, cc.ErrRunNotFound
	}
	return &r, nil
}

func (f *fakeRuns) FindByOccurrence(_ context.Context, campaignID string, occurrence time.Time) (*cc.Run, error) {
	return nil, cc.ErrRunNotFound
}

func (f *fakeRuns) FindActive(_ context.Context, campaignID string) (*cc.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.byID {
		if r.CampaignID == campaignID && (r.Status == campaign.StatusRunning || r.Status == campaign.StatusPaused) {
			return &r, nil
		}
	}
	return nil, cc.ErrRunNotFound
}

func (f *fakeRuns) ListByCampaign(_ context.Context, campaignID string, page shared.Pagination) (*shared.PaginatedResult[*cc.Run], error) {
	return nil, nil
}

func (f *fakeRuns) ListByStatus(_ context.Context, statuses ...campaign.Status) ([]*cc.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*cc.Run
	for _, r := range f.byID {
		if slices.Contains(statuses, r.Status) {
			copied := r
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (f *fakeRuns) SwapStatus(_ context.Context, id string, from, to campaign.Status) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok || r.Status != from {
		return false, nil
	}
	r.Status = to
	f.byID[id] = r
	return true, nil
}

func (f *fakeRuns) Complete(_ context.Context, id string, at time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok || r.Status != campaign.StatusRunning {
		return false, nil
	}
	r.Status, r.CompletedAt = campaign.StatusCompleted, &at
	f.byID[id] = r
	return true, nil
}

func (f *fakeRuns) CountByStatus(_ context.Context, campaignID string) (map[campaign.Status]int64, error) {
	return nil, nil
}

type fakeRecipients struct {
	mu        sync.Mutex
	byID      map[string]cc.Recipient
	order     []string
	createErr error
}

func newFakeRecipients() *fakeRecipients { return &fakeRecipients{byID: map[string]cc.Recipient{}} }

func (f *fakeRecipients) put(r cc.Recipient) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[r.ID]; !ok {
		f.order = append(f.order, r.ID)
	}
	f.byID[r.ID] = r
}

func (f *fakeRecipients) get(id string) cc.Recipient {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byID[id]
}

func (f *fakeRecipients) ofRun(runID string) []cc.Recipient {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []cc.Recipient
	for _, id := range f.order {
		if r := f.byID[id]; r.RunID == runID {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeRecipients) CreateMany(_ context.Context, recipients []cc.Recipient) error {
	if f.createErr != nil {
		return f.createErr
	}
	for _, r := range recipients {
		f.put(r)
	}
	return nil
}

func (f *fakeRecipients) FindByID(_ context.Context, id string) (*cc.Recipient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return nil, cc.ErrRecipientNotFound
	}
	return &r, nil
}

func (f *fakeRecipients) FindByMessageID(_ context.Context, messageID string) (*cc.Recipient, error) {
	return nil, cc.ErrRecipientNotFound
}

func (f *fakeRecipients) settle(id string, apply func(cc.Recipient) (cc.Recipient, error)) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return false, cc.ErrRecipientNotFound
	}
	if r.Status != cc.RecipientPending {
		return false, nil
	}
	next, err := apply(r)
	if err != nil {
		return false, err
	}
	f.byID[id] = next
	return true, nil
}

func (f *fakeRecipients) MarkSent(_ context.Context, id, messageID string, at time.Time) (bool, error) {
	return f.settle(id, func(r cc.Recipient) (cc.Recipient, error) { return r.MarkSent(messageID, at) })
}

func (f *fakeRecipients) MarkSkipped(_ context.Context, id string, reason cc.SkipReason) (bool, error) {
	return f.settle(id, func(r cc.Recipient) (cc.Recipient, error) { return r.Skip(reason) })
}

func (f *fakeRecipients) MarkFailed(_ context.Context, id, code string) (bool, error) {
	return f.settle(id, func(r cc.Recipient) (cc.Recipient, error) { return r.Fail(code) })
}

func (f *fakeRecipients) PendingIDs(_ context.Context, runID, after string, limit int) ([]string, error) {
	return nil, nil
}

func (f *fakeRecipients) List(_ context.Context, in cc.ListRecipientsInput) (*shared.PaginatedResult[*cc.Recipient], error) {
	return nil, nil
}

func (f *fakeRecipients) Counts(_ context.Context, runID string) (cc.RecipientCounts, error) {
	counts := cc.RecipientCounts{Skipped: map[cc.SkipReason]int64{}}
	for _, r := range f.ofRun(runID) {
		counts.Total++
		switch r.Status {
		case cc.RecipientPending:
			counts.Pending++
		case cc.RecipientSent:
			counts.Sent++
		case cc.RecipientFailed:
			counts.Failed++
		case cc.RecipientSkipped:
			counts.Skipped[r.SkipReason]++
		}
	}
	return counts, nil
}

func (f *fakeRecipients) CountsByVariant(_ context.Context, runID string) (map[int]cc.RecipientCounts, error) {
	return nil, nil
}

type fakeFunnels struct {
	funnel   cc.Funnel
	variants map[int]cc.Funnel
	taps     map[rcs.ButtonKey]int64
	clicks   map[rcs.ButtonKey]int64
	carriers []cc.CarrierStat
	touches  []cc.Touch
	queries  []cc.FunnelQuery
	since    time.Time
}

func (f *fakeFunnels) Funnel(_ context.Context, q cc.FunnelQuery) (cc.Funnel, error) {
	f.queries = append(f.queries, q)
	return f.funnel, nil
}

func (f *fakeFunnels) VariantFunnels(_ context.Context, q cc.FunnelQuery) (map[int]cc.Funnel, error) {
	f.queries = append(f.queries, q)
	return f.variants, nil
}

func (f *fakeFunnels) ButtonCounts(_ context.Context, q cc.FunnelQuery, variant int) (map[rcs.ButtonKey]int64, map[rcs.ButtonKey]int64, error) {
	f.queries = append(f.queries, q)
	return f.taps, f.clicks, nil
}

func (f *fakeFunnels) Carriers(_ context.Context, q cc.FunnelQuery) ([]cc.CarrierStat, error) {
	f.queries = append(f.queries, q)
	return f.carriers, nil
}

func (f *fakeFunnels) Touches(_ context.Context, ws, leadID string, since time.Time) ([]cc.Touch, error) {
	f.since = since
	return f.touches, nil
}

type fakeSelections struct {
	ids       []string
	snapshots map[string][]string
	dropped   []string
	scopes    []selection.Scope
}

func (f *fakeSelections) Selected(_ context.Context, scope selection.Scope, s selection.Selection) (int, error) {
	f.scopes = append(f.scopes, scope)
	return len(f.ids), nil
}

func (f *fakeSelections) Freeze(_ context.Context, scope selection.Scope, s selection.Selection, snapshotID string, pending *lead.Assignment) (lead.Frozen, error) {
	if f.snapshots == nil {
		f.snapshots = map[string][]string{}
	}
	ids := append([]string(nil), f.ids...)
	sort.Strings(ids)
	f.snapshots[snapshotID] = ids
	return lead.Frozen{Size: len(ids)}, nil
}

func (f *fakeSelections) Snapshot(_ context.Context, ws, snapshotID, after string, limit int) ([]string, error) {
	var out []string
	for _, id := range f.snapshots[snapshotID] {
		if id > after && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeSelections) DropSnapshot(_ context.Context, ws, snapshotID string) error {
	f.dropped = append(f.dropped, snapshotID)
	return nil
}

type fakeLeads struct {
	byID map[string]*lead.Lead
}

func (f *fakeLeads) FindByIDs(ws string, ids []string) ([]*lead.Lead, error) {
	var out []*lead.Lead
	for _, id := range ids {
		if l, ok := f.byID[id]; ok && l.WorkspaceID == ws {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeLeads) FindByNumber(ws, number string) (*lead.Lead, error) {
	for _, l := range f.byID {
		if l.WorkspaceID == ws && shared.NormalizePhone(l.Number) == number {
			return l, nil
		}
	}
	return nil, lead.ErrLeadNotFound
}

type fakeContacts struct{}

func (fakeContacts) AttachContactDetails(context.Context, string, []*lead.Lead) error { return nil }

type fakeNames struct{}

func (fakeNames) Names(ids ...string) map[string]string {
	out := map[string]string{}
	for _, id := range ids {
		out[id] = "Dona " + id
	}
	return out
}

type fakeDefinitions struct{ defs []*customfield.Definition }

func (f fakeDefinitions) ListByObject(string, customfield.ObjectType) ([]*customfield.Definition, error) {
	return f.defs, nil
}

type fakeSuppressions struct{ phones map[string]bool }

func (f fakeSuppressions) SuppressedAmong(_ context.Context, ws string, phones []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, p := range phones {
		if f.phones[p] {
			out[p] = true
		}
	}
	return out, nil
}

type fakeSenders struct{ byID map[string]*rcs.Sender }

func (f fakeSenders) FindByID(_ context.Context, ws, id string) (*rcs.Sender, error) {
	s, ok := f.byID[id]
	if !ok || s.WorkspaceID != ws {
		return nil, rcs.ErrSenderNotFound
	}
	return s, nil
}

type fakeRoutes struct{ byID map[string]*sms.Route }

func (f fakeRoutes) FindByID(_ context.Context, ws, id string) (*sms.Route, error) {
	r, ok := f.byID[id]
	if !ok || r.WorkspaceID != ws {
		return nil, sms.ErrRouteNotFound
	}
	return r, nil
}

type fakeMessages struct {
	byID map[string]*carriermessaging.Message
}

func (f *fakeMessages) FindByID(_ context.Context, id string) (*carriermessaging.Message, error) {
	m, ok := f.byID[id]
	if !ok {
		return nil, carriermessaging.ErrMessageNotFound
	}
	return m, nil
}

type fakeCarrier struct {
	mu       sync.Mutex
	requests []carriermessaging_usecase.DeliverRequest
	err      error
}

func (f *fakeCarrier) Deliver(_ context.Context, req carriermessaging_usecase.DeliverRequest) (*carriermessaging.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	return &carriermessaging.Message{ID: req.MessageID, Channel: req.Channel, Status: carriermessaging.StatusAccepted}, nil
}

type fakePauser struct {
	paused []string
	reason rcs.PauseReason
}

func (f *fakePauser) AdminPauseSender(_ context.Context, id string, reason rcs.PauseReason) (*rcs.Sender, error) {
	f.paused = append(f.paused, id)
	f.reason = reason
	return &rcs.Sender{ID: id}, nil
}

type fakeSettings struct{ days int }

func (f fakeSettings) GetSettings(_ context.Context, ws string) (carriermessaging.Settings, error) {
	return carriermessaging.Settings{WorkspaceID: ws, AttributionDays: f.days}, nil
}

type fakePrices map[carriermessaging.ChargeKind]int64

func (f fakePrices) Price(ws string, kind carriermessaging.ChargeKind) (workspace_pricing.PriceResult, error) {
	price, ok := f[kind]
	if !ok {
		return workspace_pricing.PriceResult{}, fmt.Errorf("%w: %s", carriermessaging.ErrPriceUnavailable, kind)
	}
	return workspace_pricing.PriceResult{PriceMicros: price}, nil
}

type fakeBalance struct{ micros int64 }

func (f fakeBalance) GetBalance(string) (int64, error) { return f.micros, nil }

type fakeSpend struct{ micros int64 }

func (f fakeSpend) SpendSince(context.Context, string, time.Time) (int64, error) {
	return f.micros, nil
}

type fakeQueue struct {
	mu         sync.Mutex
	calls      []string
	subscribed map[string]bool
}

func (f *fakeQueue) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeQueue) SubscribeToCampaign(id string) error {
	f.record("subscribe:" + id)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subscribed == nil {
		f.subscribed = map[string]bool{}
	}
	f.subscribed[id] = true
	return nil
}

func (f *fakeQueue) PauseCampaignConsumer(id string) error {
	f.record("pause:" + id)
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.subscribed, id)
	return nil
}

func (f *fakeQueue) ResumeCampaignConsumer(id string) error { f.record("resume:" + id); return nil }

func (f *fakeQueue) StopCampaignConsumer(id string) error { f.record("stop:" + id); return nil }

func (f *fakeQueue) IsSubscribed(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subscribed[id]
}

func (f *fakeQueue) SeedCounterIfMissing(id string) { f.record("seed:" + id) }

func (f *fakeQueue) has(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.calls, call)
}

type fakeEnqueuer struct {
	queue    *fakeQueue
	runID    string
	messages []campaignqueue.Message
}

func (f *fakeEnqueuer) Enqueue(runID string, messages []campaignqueue.Message) error {
	if f.queue != nil {
		f.queue.record("enqueue:" + runID)
	}
	f.runID = runID
	f.messages = append(f.messages, messages...)
	return nil
}

type fakeAttempts struct {
	counts map[string]int64
	err    error
}

func (f *fakeAttempts) Incr(key string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	if f.counts == nil {
		f.counts = map[string]int64{}
	}
	f.counts[key]++
	return f.counts[key], nil
}

func (f *fakeAttempts) Del(keys ...string) error {
	for _, k := range keys {
		delete(f.counts, k)
	}
	return nil
}

var (
	testNow = time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	wsID    = "ws1"
	owner   = "user-1"
)

type harness struct {
	svc        *Service
	campaigns  *fakeCampaigns
	runs       *fakeRuns
	recipients *fakeRecipients
	funnels    *fakeFunnels
	selections *fakeSelections
	leads      *fakeLeads
	suppressed map[string]bool
	senders    map[string]*rcs.Sender
	routes     map[string]*sms.Route
	messages   *fakeMessages
	carrier    *fakeCarrier
	pauser     *fakePauser
	prices     fakePrices
	balance    *fakeBalance
	queue      *fakeQueue
	enqueuer   *fakeEnqueuer
	attempts   *fakeAttempts
	stats      *fakeStats
	timeline   *fakeTimeline
	rows       *fakeRows
	overview   *fakeOverview
	whatsapp   *fakeWhatsAppSpend
	now        time.Time
	ids        int
}

func newHarness(t interface{ Fatal(...any) }) *harness {
	h := &harness{
		campaigns:  newFakeCampaigns(),
		runs:       newFakeRuns(),
		recipients: newFakeRecipients(),
		funnels:    &fakeFunnels{},
		selections: &fakeSelections{},
		leads:      &fakeLeads{byID: map[string]*lead.Lead{}},
		suppressed: map[string]bool{},
		senders: map[string]*rcs.Sender{
			"sender-1": {ID: "sender-1", WorkspaceID: wsID, Status: rcs.SenderLaunched},
		},
		routes: map[string]*sms.Route{
			"route-1": {ID: "route-1", WorkspaceID: wsID, BrandPrefix: "Loja", Kind: sms.RouteShared, Status: sms.RouteActive},
		},
		messages: &fakeMessages{byID: map[string]*carriermessaging.Message{}},
		carrier:  &fakeCarrier{},
		pauser:   &fakePauser{},
		prices: fakePrices{
			carriermessaging.ChargeRCSBasic:   10_000,
			carriermessaging.ChargeRCSSingle:  25_000,
			carriermessaging.ChargeRCSInbound: 10_000,
			carriermessaging.ChargeSMSSegment: 40_000,
			carriermessaging.ChargeSMSInbound: 8_000,
		},
		balance:  &fakeBalance{micros: 100_000_000},
		queue:    &fakeQueue{},
		attempts: &fakeAttempts{},
		stats:    &fakeStats{},
		timeline: &fakeTimeline{},
		rows:     &fakeRows{},
		overview: &fakeOverview{},
		whatsapp: &fakeWhatsAppSpend{},
		now:      testNow,
	}
	h.enqueuer = &fakeEnqueuer{queue: h.queue}
	svc, err := New(Deps{
		Campaigns:     h.campaigns,
		Runs:          h.runs,
		Recipients:    h.recipients,
		Funnels:       h.funnels,
		Selections:    h.selections,
		Leads:         h.leads,
		Contacts:      fakeContacts{},
		Names:         fakeNames{},
		Definitions:   fakeDefinitions{},
		Suppressions:  fakeSuppressions{phones: h.suppressed},
		Senders:       fakeSenders{byID: h.senders},
		Routes:        fakeRoutes{byID: h.routes},
		Messages:      h.messages,
		Carrier:       h.carrier,
		SenderPauser:  h.pauser,
		Settings:      fakeSettings{days: 7},
		Prices:        h.prices,
		Balance:       h.balance,
		Spend:         fakeSpend{},
		Enqueuer:      h.enqueuer,
		Attempts:      h.attempts,
		Stats:         h.stats,
		Timeline:      h.timeline,
		RecipientRows: h.rows,
		Overview:      h.overview,
		WhatsAppSpend: h.whatsapp,
		Timezones:     fakeTimezones{},
		Now:           func() time.Time { return h.now },
		NewID:         func() string { h.ids++; return "id-" + strconv.Itoa(1000+h.ids) },
		Logf:          func(string, ...any) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc.launched = func(carriermessaging.Channel) bool { return true }
	svc.AttachQueue(h.queue)
	h.svc = svc
	h.wireAudience(t)
	return h
}

func phoneOf(n int) string {
	return fmt.Sprintf("+55 11 99999-%04d", n)
}

func (h *harness) addLead(id string, n int, mutate ...func(*lead.Lead)) *lead.Lead {
	l := &lead.Lead{ID: id, WorkspaceID: wsID, Name: "Ana Souza", Number: phoneOf(n), Owner: "owner-1"}
	for _, m := range mutate {
		m(l)
	}
	h.leads.byID[id] = l
	h.selections.ids = append(h.selections.ids, id)
	return l
}

func firstNameBinding() campaign.VariableBinding {
	return campaign.VariableBinding{Source: campaign.BindFirstName}
}

func rcsCampaign(id string) cc.Campaign {
	return cc.Campaign{
		ID:          id,
		WorkspaceID: wsID,
		Name:        "Oferta",
		SenderID:    "sender-1",
		Channel:     carriermessaging.ChannelRCS,
		Status:      cc.StatusDraft,
		Kind:        cc.KindMarketing,
		Variants:    []cc.Variant{{Content: rcs.Content{Text: "Oi {{1}}, temos novidades"}, Bindings: []campaign.VariableBinding{firstNameBinding()}}},
		Audience:    selection.Selection{Mode: selection.ModeIDs, IDs: []string{"lead-a"}},
		CreatedBy:   owner,
	}
}

func smsCampaign(id string) cc.Campaign {
	c := rcsCampaign(id)
	c.Channel = carriermessaging.ChannelSMS
	c.SenderID = "route-1"
	c.Variants = []cc.Variant{{Text: "Oi {{1}}, temos novidades", Bindings: []campaign.VariableBinding{firstNameBinding()}}}
	return c
}

func (h *harness) runningRun(c cc.Campaign, runID string) cc.Run {
	c.Status = cc.StatusRunning
	h.campaigns.put(c)
	r := cc.Run{ID: runID, WorkspaceID: c.WorkspaceID, CampaignID: c.ID, Occurrence: h.now, Status: campaign.StatusRunning, StartedAt: h.now}
	h.runs.put(r)
	return r
}

func (h *harness) pendingRecipient(r cc.Run, id, leadID string, n int) cc.Recipient {
	rec := cc.Recipient{ID: id, WorkspaceID: r.WorkspaceID, CampaignID: r.CampaignID, RunID: r.ID, LeadID: leadID, Phone: shared.NormalizePhone(phoneOf(n)), Status: cc.RecipientPending}
	h.recipients.put(rec)
	return rec
}
