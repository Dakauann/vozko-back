package leadexit_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/buttonaction"
	"vozko/domain/calls/calllist"
	ce "vozko/domain/conversation_event"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	"vozko/domain/leadexit"
	"vozko/domain/recordevent"
	"vozko/domain/shared"
	buttonaction_usecase "vozko/usecases/buttonaction"
	lead_usecase "vozko/usecases/lead"
	opportunity_usecase "vozko/usecases/opportunity"
	workflow_usecase "vozko/usecases/workflow"
)

const (
	workspaceA = "11111111-1111-1111-1111-111111111111"
	leadNumber = "5511987654321"
	otherPhone = "5511912345678"
)

var (
	stepNow  = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	promoRef = buttonaction.TemplateRef{Name: "promo_outubro", Language: "pt_BR"}
)

type memLeads struct {
	mu        sync.Mutex
	leads     map[string]lead.Lead
	saves     int
	events    []recordevent.Event
	failSaves int
}

func newMemLeads(leads ...*lead.Lead) *memLeads {
	m := &memLeads{leads: map[string]lead.Lead{}}
	for _, l := range leads {
		m.leads[l.ID] = *l
	}
	return m
}

func (m *memLeads) FindByID(workspaceID, id string) (*lead.Lead, error) {
	return m.Load(context.Background(), workspaceID, id)
}

func (m *memLeads) Load(_ context.Context, workspaceID, id string) (*lead.Lead, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.leads[id]
	if !ok || l.WorkspaceID != workspaceID {
		return nil, lead.ErrLeadNotFound
	}
	return &l, nil
}

func (m *memLeads) Insert(context.Context, *lead.Lead, []recordevent.Event) error {
	return errors.New("not used")
}

func (m *memLeads) Save(_ context.Context, l *lead.Lead, expectedVersion int64, events []recordevent.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failSaves > 0 {
		m.failSaves--
		return errStoreDown
	}
	if m.leads[l.ID].Version != expectedVersion {
		return shared.ErrVersionConflict
	}
	l.Version = expectedVersion + 1
	m.leads[l.ID] = *l
	m.saves++
	m.events = append(m.events, events...)
	return nil
}

func (m *memLeads) get(t *testing.T, id string) *lead.Lead {
	t.Helper()
	l, err := m.Load(context.Background(), workspaceA, id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

type quietNotifier struct{}

func (quietNotifier) LeadChanged(lead.Change) {}

func storedLead(id, number string) *lead.Lead {
	return &lead.Lead{ID: id, WorkspaceID: workspaceA, Number: number, Version: 1}
}

type fakeButtons struct {
	taps      map[string]buttonaction.Resolution
	exits     map[string]*buttonaction_usecase.TemplateExit
	tapCalls  []buttonaction_usecase.TapInput
	exitCalls int
	err       error
}

func tapKey(templateID, payload string) string { return templateID + "|" + payload }

func exitKey(k buttonaction_usecase.TemplateKey) string {
	if k.TemplateID != "" {
		return k.TemplateID
	}
	return k.TemplateName + ":" + k.Language
}

func (f *fakeButtons) ResolveTap(_ context.Context, in buttonaction_usecase.TapInput) (buttonaction.Resolution, error) {
	f.tapCalls = append(f.tapCalls, in)
	if f.err != nil {
		return buttonaction.Resolution{}, f.err
	}
	if r, ok := f.taps[tapKey(in.TemplateID, in.Payload)]; ok {
		return r, nil
	}
	return buttonaction.Resolution{Reason: buttonaction.ReasonNoMatch}, nil
}

func (f *fakeButtons) TemplateExit(_ context.Context, key buttonaction_usecase.TemplateKey) (*buttonaction_usecase.TemplateExit, error) {
	f.exitCalls++
	if f.err != nil {
		return nil, f.err
	}
	if e, ok := f.exits[exitKey(key)]; ok {
		return e, nil
	}
	return nil, buttonaction.ErrTemplateNotFound
}

type fakeSent map[string]*leadexit.SentTemplate

func (f fakeSent) SentTemplate(_ context.Context, messageID string) (*leadexit.SentTemplate, error) {
	return f[messageID], nil
}

var errStoreDown = errors.New("store down")

type fakeCampaignMessages struct {
	last  map[string]*leadexit.SentCampaignMessage
	err   error
	calls []string
}

func (f *fakeCampaignMessages) LastCampaignMessage(_ context.Context, entryType, entryID string) (*leadexit.SentCampaignMessage, error) {
	f.calls = append(f.calls, entryType+"|"+entryID)
	if f.err != nil {
		return nil, f.err
	}
	return f.last[entryType+"|"+entryID], nil
}

type memRuns struct {
	mu          sync.Mutex
	runs        map[string]leadexit.Run
	claimedAt   map[string]time.Time
	now         func() time.Time
	failFinish  int
	finishCtxOK []bool
}

func newMemRuns(now func() time.Time) *memRuns {
	return &memRuns{runs: map[string]leadexit.Run{}, claimedAt: map[string]time.Time{}, now: now}
}

func (m *memRuns) Claim(_ context.Context, run leadexit.Run) (leadexit.RunClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := run.WorkspaceID + "|" + run.InboundMessageID
	if previous, ok := m.runs[key]; ok && !leadexit.Reclaimable(previous.Outcome, m.claimedAt[key], m.now()) {
		return leadexit.RunClaim{Previous: previous}, nil
	}
	run.Outcome = leadexit.OutcomeRunning
	m.runs[key] = run
	m.claimedAt[key] = m.now()
	return leadexit.RunClaim{Claimed: true}, nil
}

func (m *memRuns) Finish(ctx context.Context, run leadexit.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.finishCtxOK = append(m.finishCtxOK, ctx.Err() == nil)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if m.failFinish > 0 {
		m.failFinish--
		return errStoreDown
	}
	m.runs[run.WorkspaceID+"|"+run.InboundMessageID] = run
	return nil
}

func (m *memRuns) of(messageID string) leadexit.Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runs[workspaceA+"|"+messageID]
}

type fakeHandOff struct {
	roulette    []ia.RouletteHandOff
	humans      []string
	err         error
	drawnButErr error
}

func (f *fakeHandOff) HandOffToHuman(workspaceID, entryID, entryType, toUserID string) error {
	f.humans = append(f.humans, toUserID)
	return f.err
}

func (f *fakeHandOff) HandOffToRoulette(in ia.RouletteHandOff) (string, error) {
	f.roulette = append(f.roulette, in)
	if f.err != nil {
		return "", f.err
	}
	return "user-9", f.drawnButErr
}

type fakeProfiles struct {
	updates []lead_usecase.ProfileUpdate
}

func (f *fakeProfiles) Update(_ context.Context, in lead_usecase.ProfileUpdate) (lead_usecase.ProfileResult, error) {
	f.updates = append(f.updates, in)
	changed := []string{}
	for key := range in.Profile.CustomFields {
		changed = append(changed, key)
	}
	return lead_usecase.ProfileResult{LeadID: in.LeadID, Changed: changed}, nil
}

type fakeDeals struct {
	commands []opportunity_usecase.EntryCommand
}

func (f *fakeDeals) ManageForEntry(_ string, cmd opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryResult, error) {
	f.commands = append(f.commands, cmd)
	return &opportunity_usecase.EntryResult{Created: true}, nil
}

type fakeWorkflows struct {
	starts  []workflow_usecase.ConversationStart
	started workflow_usecase.StartedRun
	err     error
}

func (f *fakeWorkflows) StartForConversation(in workflow_usecase.ConversationStart) (workflow_usecase.StartedRun, error) {
	f.starts = append(f.starts, in)
	if f.err != nil {
		return workflow_usecase.StartedRun{}, f.err
	}
	if f.started.RunID == "" {
		return workflow_usecase.StartedRun{RunID: "run-new"}, nil
	}
	return f.started, nil
}

type fakeCallLists struct {
	adds         []calllist.LeadAddition
	alreadyThere bool
	err          error
}

func (f *fakeCallLists) AddLead(_ context.Context, add calllist.LeadAddition) (bool, error) {
	f.adds = append(f.adds, add)
	if f.err != nil {
		return false, f.err
	}
	return !f.alreadyThere, nil
}

type fakeEvents struct {
	mu     sync.Mutex
	events []*ce.ConversationEvent
}

func (f *fakeEvents) Log(e *ce.ConversationEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

func (f *fakeEvents) last(t *testing.T) map[string]string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.events) == 0 {
		t.Fatal("no conversation line written")
	}
	return f.events[len(f.events)-1].DetailsMap()
}

type fakeReplier struct {
	sent   []string
	err    error
	during func()
}

func (f *fakeReplier) Reply(_ context.Context, text string) error {
	if f.during != nil {
		f.during()
	}
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, text)
	return nil
}

type fixture struct {
	step      *Step
	leads     *memLeads
	buttons   *fakeButtons
	sent      fakeSent
	campaigns *fakeCampaignMessages
	runs      *memRuns
	handOff   *fakeHandOff
	profiles  *fakeProfiles
	deals     *fakeDeals
	workflows *fakeWorkflows
	callLists *fakeCallLists
	events    *fakeEvents
	now       time.Time
}

func newFixture(t *testing.T, leads ...*lead.Lead) *fixture {
	t.Helper()
	f := &fixture{
		leads:     newMemLeads(leads...),
		buttons:   &fakeButtons{taps: map[string]buttonaction.Resolution{}, exits: map[string]*buttonaction_usecase.TemplateExit{}},
		sent:      fakeSent{},
		campaigns: &fakeCampaignMessages{last: map[string]*leadexit.SentCampaignMessage{}},
		runs:      newMemRuns(func() time.Time { return stepNow }),
		handOff:   &fakeHandOff{},
		profiles:  &fakeProfiles{},
		deals:     &fakeDeals{},
		workflows: &fakeWorkflows{},
		callLists: &fakeCallLists{},
		events:    &fakeEvents{},
		now:       stepNow,
	}
	ids := 0
	consent, err := lead_usecase.NewConsentCommands(lead_usecase.ConsentDeps{
		Store: f.leads, Notifier: quietNotifier{}, Now: func() time.Time { return f.now },
		NewID: func() string { ids++; return "consent-ev-" + string(rune('0'+ids)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	step, err := NewStep(Deps{
		Buttons: f.buttons, Sent: f.sent, Campaigns: f.campaigns, Runs: f.runs, Consent: consent, Leads: f.leads,
		HandOff: f.handOff, Profiles: f.profiles, Deals: f.deals, Workflows: f.workflows, CallLists: f.callLists, Events: f.events,
		Now: func() time.Time { return f.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.step = step
	f.runs.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) sentPromo(messageID, recipient string) {
	f.sent[messageID] = &leadexit.SentTemplate{
		MessageID: messageID, WorkspaceID: workspaceA, RecipientNumber: recipient, TemplateID: "tmpl-1", Template: promoRef, CampaignID: "camp-1",
	}
}

func (f *fixture) promoButton(payload string, action buttonaction.ButtonAction) {
	action.Template = promoRef
	f.buttons.taps[tapKey("tmpl-1", payload)] = buttonaction.Resolution{
		Template: promoRef, ButtonIndex: action.ButtonIndex, Action: &action, Source: buttonaction.SourceTemplate, Reason: buttonaction.ReasonResolved,
	}
}

func tapOn(messageID, contextID, payload, text string) leadexit.Inbound {
	return leadexit.Inbound{
		WorkspaceID: workspaceA, LeadID: "lead-1", SenderNumber: leadNumber, EntryID: "entry-1", EntryType: "whatsapp", Channel: "whatsapp",
		MessageID: messageID, Text: text, ReceivedAt: stepNow, Tap: &leadexit.Tap{Payload: payload, Text: text}, ContextMessageID: contextID,
	}
}

func (f *fixture) promoShows(word, confirmation string) {
	f.buttons.exits["promo_outubro:pt_BR"] = &buttonaction_usecase.TemplateExit{Template: promoRef, Words: []string{word}, Confirmation: confirmation}
}

func afterPromo(in leadexit.Inbound) leadexit.Inbound {
	in.LastTemplate = &leadexit.SentTemplate{Template: promoRef}
	return in
}

func typed(messageID, text string) leadexit.Inbound {
	return leadexit.Inbound{
		WorkspaceID: workspaceA, LeadID: "lead-1", SenderNumber: leadNumber, EntryID: "entry-1", EntryType: "whatsapp", Channel: "whatsapp",
		MessageID: messageID, Text: text, ReceivedAt: stepNow,
	}
}
