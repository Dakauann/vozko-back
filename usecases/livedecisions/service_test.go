package livedecisions_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/audience"
	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
	"vozko/domain/shared"
)

var t0 = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

type fakeModel struct {
	mu       sync.Mutex
	result   decision.Result
	err      error
	requests []decision.Request
}

func (f *fakeModel) Decide(_ context.Context, request decision.Request) (decision.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request)
	return f.result, f.err
}

func (f *fakeModel) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

type fakeSnapshots struct{ snapshot ld.Snapshot }

func (f fakeSnapshots) Snapshot(_ context.Context, trigger Trigger, _ bool) (ld.Snapshot, error) {
	s := f.snapshot
	s.WorkspaceID, s.EntryID, s.EntryType = trigger.WorkspaceID, trigger.EntryID, string(trigger.EntryType)
	return s, nil
}

type fakeReads struct {
	mu    sync.Mutex
	reads map[string]ld.LiveRead
	stale bool
}

func (f *fakeReads) Save(_ context.Context, read ld.LiveRead) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stale {
		return false, nil
	}
	if f.reads == nil {
		f.reads = map[string]ld.LiveRead{}
	}
	f.reads[read.EntryID] = read
	return true, nil
}
func (f *fakeReads) Get(_ context.Context, _, entryID, _ string) (*ld.LiveRead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	read, ok := f.reads[entryID]
	if !ok {
		return nil, nil
	}
	return &read, nil
}
func (f *fakeReads) ForEntries(context.Context, string, []ld.EntryRef) (map[ld.EntryRef]ld.LiveRead, error) {
	return nil, nil
}

type fakeLog struct {
	mu      sync.Mutex
	records []ld.Record
}

func (f *fakeLog) Append(_ context.Context, r ld.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, r)
	return nil
}
func (f *fakeLog) Summarize(context.Context, time.Time) ([]ld.Summary, error) { return nil, nil }
func (f *fakeLog) last() ld.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.records[len(f.records)-1]
}

type fakeEffects struct {
	mu         sync.Mutex
	moved      []string
	certainty  []float64
	broadcasts []ld.LiveRead
	moveErr    error
}

func (f *fakeEffects) MoveStage(_ context.Context, _, _ string, _ shared.EntryType, stageID string, certainty float64) error {
	f.certainty = append(f.certainty, certainty)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.moveErr != nil {
		return f.moveErr
	}
	f.moved = append(f.moved, stageID)
	return nil
}
func (f *fakeEffects) LiveReadChanged(read ld.LiveRead) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcasts = append(f.broadcasts, read)
}

type allow bool

func (a allow) Allow(string) bool                 { return bool(a) }
func (a allow) AllowDecision(string, string) bool { return bool(a) }

type harness struct {
	service  *Service
	model    *fakeModel
	reads    *fakeReads
	log      *fakeLog
	effects  *fakeEffects
	subjects *fakeSubjects
}

type fakeSubjects struct {
	trigger Trigger
	found   bool
	err     error
	asked   []ld.EntryRef
}

func (f *fakeSubjects) Subject(_ context.Context, ref ld.EntryRef) (Trigger, bool, error) {
	f.asked = append(f.asked, ref)
	return f.trigger, f.found, f.err
}

func snapshot() ld.Snapshot {
	return ld.Snapshot{
		Turns: []ld.Turn{
			{Role: ld.RoleAgent, Text: "O plano anual sai R$ 1.200.", At: t0},
			{Role: ld.RoleCustomer, Text: "não quero mais, pode encerrar", At: t0.Add(time.Minute)},
		},
		CurrentStageID: "st-a",
		Stages: []ld.StageOption{
			{ID: "st-a", Name: "Negociação", Description: "preço"},
			{ID: "st-b", Name: "Perdido", Description: "desistiu"},
		},
	}
}

func analysisAnswers() map[string]decision.Answer {
	return map[string]decision.Answer{
		audience.FieldInterest:                {Kind: decision.KindChoice, Choice: "not_interested", Confidence: 0.9},
		audience.FieldDisposition:             {Kind: decision.KindChoice, Choice: "declined", Confidence: 0.9},
		audience.FieldSentiment:               {Kind: decision.KindChoice, Choice: "negative", Confidence: 0.9},
		audience.FieldQualification:           {Kind: decision.KindChoice, Choice: "cold_lead", Confidence: 0.9},
		audience.FieldNextAction:              {Kind: decision.KindChoice, Choice: "close", Confidence: 0.9},
		audience.QualityKeyGoalProgress:       {Kind: decision.KindScore, Score: 0, Confidence: 0.9},
		audience.QualityKeyCustomerEngagement: {Kind: decision.KindScore, Score: 1, Confidence: 0.9},
		audience.QualityKeyAgentConduct:       {Kind: decision.KindScore, Score: 2, Confidence: 0.9},
		audience.QualityKeyProfessionalism:    {Kind: decision.KindScore, Score: 3, Confidence: 0.9},
		audience.FieldLanguage:                {Kind: decision.KindChoice, Choice: "pt", Confidence: 0.99},
	}
}

func fullResult() decision.Result {
	answers := analysisAnswers()
	answers[ld.QuestionStage] = decision.Answer{Kind: decision.KindChoice, Choice: "st-b", Confidence: 0.91}
	return decision.Result{Model: "typesafe/jev-1.13", Answers: answers, InputTokens: 2000, CostMicros: 84}
}

func newHarness() *harness {
	h := &harness{
		model:    &fakeModel{result: fullResult()},
		reads:    &fakeReads{},
		log:      &fakeLog{},
		effects:  &fakeEffects{},
		subjects: &fakeSubjects{trigger: trigger(), found: true},
	}
	h.service = NewService(Deps{
		Subjects:  h.subjects,
		Model:     h.model,
		Policy:    decision.DefaultPolicy(),
		Snapshots: fakeSnapshots{snapshot: snapshot()},
		Reads:     h.reads,
		Log:       h.log,
		Stages:    h.effects,
		Live:      h.effects,
		Funds:     allow(true),
		Limiter:   allow(true),
		Clock:     func() time.Time { return t0.Add(time.Hour) },
	})
	return h
}

func trigger() Trigger {
	return Trigger{
		WorkspaceID: "ws", EntryID: "entry", EntryType: shared.EntryTypeWhatsApp,
		Features: ld.Features{Staging: true, Analysis: true},
	}
}

func TestALiveDecisionMovesTheStageAndPublishesTheReading(t *testing.T) {
	h := newHarness()
	if _, err := h.service.Decide(context.Background(), trigger()); err != nil {
		t.Fatal(err)
	}
	if len(h.effects.moved) != 1 || h.effects.moved[0] != "st-b" {
		t.Fatalf("moved = %v", h.effects.moved)
	}
	if h.effects.certainty[0] < 0.8 {
		t.Fatalf("the move must carry the certainty Jev gave it: %v", h.effects.certainty)
	}
	read, _ := h.reads.Get(context.Background(), "ws", "entry", "whatsapp")
	if read == nil || read.Qualification != audience.QualificationColdLead || !read.StageSettled || len(h.effects.broadcasts) != 1 {
		t.Fatalf("live read = %+v, broadcasts = %d", read, len(h.effects.broadcasts))
	}
	record := h.log.last()
	if record.Purpose != ld.PurposeLive || record.CostMicros != 84 || record.Failure != "" ||
		len(record.Effects) != 1 || record.Effects[0] != ld.EffectStageMoved {
		t.Fatalf("record = %+v", record)
	}
	request := h.model.requests[0]
	if request.WorkspaceID != "ws" || request.Purpose != ld.PurposeLive || request.ReferenceID != "entry" {
		t.Fatalf("request = %+v", request)
	}
	asked := analysisAnswers()
	asked[ld.QuestionStage] = decision.Answer{}
	if len(request.Questions) != len(asked) {
		t.Fatalf("asked %d questions, want the stage and the %d analysis labels", len(request.Questions), len(asked)-1)
	}
	for id := range request.Questions {
		if _, ok := asked[id]; !ok {
			t.Fatalf("unexpected question %q", id)
		}
	}
}

func TestNothingRunsWithoutAModel(t *testing.T) {
	h := newHarness()
	h.service.deps.Model = nil
	if _, err := h.service.Decide(context.Background(), trigger()); !errors.Is(err, decision.ErrDisabled) {
		t.Fatalf("no decision model: %v", err)
	}
	if h.service.Acts(context.Background(), "ws") {
		t.Fatal("without a model everything stays as today")
	}
}

func TestNoBalanceOrTooManyDecisionsStopsBeforeTheModel(t *testing.T) {
	h := newHarness()
	h.service.deps.Funds = allow(false)
	if _, err := h.service.Decide(context.Background(), trigger()); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("no funds: %v", err)
	}
	h.service.deps.Funds = allow(true)
	h.service.deps.Limiter = allow(false)
	if _, err := h.service.Decide(context.Background(), trigger()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate limited: %v", err)
	}
	if h.model.calls() != 0 {
		t.Fatal("the model must not be called past a failed guard")
	}
}

func TestAModelFailureAppliesNothingAndIsLogged(t *testing.T) {
	h := newHarness()
	h.model.err = decision.ErrUnavailable
	if _, err := h.service.Decide(context.Background(), trigger()); !errors.Is(err, decision.ErrUnavailable) {
		t.Fatalf("Decide() = %v", err)
	}
	if len(h.effects.moved) != 0 {
		t.Fatal("a failed decision must not act")
	}
	if h.log.last().Failure == "" {
		t.Fatal("the failure must be logged")
	}
}

func TestAStaleDecisionIsDiscardedWithoutActing(t *testing.T) {
	h := newHarness()
	h.reads.stale = true
	if _, err := h.service.Decide(context.Background(), trigger()); err != nil {
		t.Fatal(err)
	}
	if len(h.effects.moved) != 0 {
		t.Fatal("a newer decision already landed; this one must not act")
	}
	if h.log.last().Failure != FailureStale {
		t.Fatalf("record = %+v", h.log.last())
	}
}

func TestAFailedStageMoveIsNotLoggedAsApplied(t *testing.T) {
	h := newHarness()
	h.effects.moveErr = errors.New("stage gone")
	if _, err := h.service.Decide(context.Background(), trigger()); err != nil {
		t.Fatal(err)
	}
	if effects := h.log.last().Effects; len(effects) != 0 {
		t.Fatalf("a stage move that failed was logged as applied: %v", effects)
	}
}

func TestNothingToAskMakesNoCall(t *testing.T) {
	h := newHarness()
	tr := trigger()
	tr.Features = ld.Features{}
	if _, err := h.service.Decide(context.Background(), tr); err != nil || h.model.calls() != 0 {
		t.Fatalf("calls = %d, err = %v", h.model.calls(), err)
	}
}
