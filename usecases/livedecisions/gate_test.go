package livedecisions_usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/decision"
	ld "vozko/domain/livedecision"
	"vozko/domain/shared"
)

type memoryState struct {
	cache.SharedState
	mu      sync.Mutex
	strings map[string]string
	hashes  map[string]map[string]string
}

func newMemoryState() *memoryState {
	return &memoryState{strings: map[string]string{}, hashes: map[string]map[string]string{}}
}

func (m *memoryState) SetNX(key, value string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.strings[key]; ok {
		return false, nil
	}
	m.strings[key] = value
	return true, nil
}
func (m *memoryState) GetString(key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.strings[key], nil
}
func (m *memoryState) Del(keys ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.strings, k)
	}
	return nil
}
func (m *memoryState) HSet(key, field, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hashes[key] == nil {
		m.hashes[key] = map[string]string{}
	}
	m.hashes[key][field] = value
	return nil
}
func (m *memoryState) HGetAll(key string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.hashes[key] {
		out[k] = v
	}
	return out, nil
}
func (m *memoryState) HDelIfValue(key, field, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hashes[key][field] == value {
		delete(m.hashes[key], field)
	}
	return nil
}
func (m *memoryState) pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.hashes[pendingKey])
}

type recordedDecisions struct {
	mu   sync.Mutex
	refs []ld.EntryRef
}

func (r *recordedDecisions) decide(_ context.Context, ref ld.EntryRef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refs = append(r.refs, ref)
}
func (r *recordedDecisions) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.refs)
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newCoalescer(t *testing.T, state *memoryState, decisions *recordedDecisions, c *clock) *Coalescer {
	t.Helper()
	coalescer, err := NewCoalescer(state, decisions.decide, CoalescerConfig{Quiet: 4 * time.Second, MaxWait: 15 * time.Second, Workers: 2, Clock: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	return coalescer
}

func entry() ld.EntryRef {
	return ld.EntryRef{EntryID: "entry", EntryType: string(shared.EntryTypeWhatsApp)}
}

func TestABurstOfMessagesBecomesOneDecisionAfterTheQuietGap(t *testing.T) {
	state, decisions, c := newMemoryState(), &recordedDecisions{}, &clock{now: t0}
	coalescer := newCoalescer(t, state, decisions, c)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := coalescer.Stamp(entry()); err != nil {
			t.Fatal(err)
		}
		c.advance(time.Second)
	}
	if n := coalescer.Flush(ctx); n != 0 || decisions.count() != 0 {
		t.Fatalf("decided %d before the quiet gap", n)
	}
	c.advance(3 * time.Second)
	if n := coalescer.Flush(ctx); n != 1 || decisions.count() != 1 {
		t.Fatalf("decided %d times, want exactly once", decisions.count())
	}
	if decisions.refs[0] != entry() {
		t.Fatalf("entry = %+v", decisions.refs[0])
	}
	if state.pending() != 0 || coalescer.Flush(ctx) != 0 {
		t.Fatal("a decided entry must leave the queue")
	}
}

func TestAnEndlessBurstIsDecidedAtTheMaximumWait(t *testing.T) {
	state, decisions, c := newMemoryState(), &recordedDecisions{}, &clock{now: t0}
	coalescer := newCoalescer(t, state, decisions, c)
	ctx := context.Background()
	for i := 0; i < 16; i++ {
		_ = coalescer.Stamp(entry())
		c.advance(time.Second)
		coalescer.Flush(ctx)
	}
	if decisions.count() != 1 {
		t.Fatalf("a customer who never pauses is still read after 15 s: decided %d times", decisions.count())
	}
}

func TestTwoWorkersNeverDecideTheSameBurstTwice(t *testing.T) {
	state, decisions, c := newMemoryState(), &recordedDecisions{}, &clock{now: t0}
	first, second := newCoalescer(t, state, decisions, c), newCoalescer(t, state, decisions, c)
	_ = first.Stamp(entry())
	c.advance(5 * time.Second)

	var wg sync.WaitGroup
	for _, coalescer := range []*Coalescer{first, second} {
		wg.Add(1)
		go func(co *Coalescer) {
			defer wg.Done()
			co.Flush(context.Background())
		}(coalescer)
	}
	wg.Wait()
	if decisions.count() != 1 {
		t.Fatalf("decided %d times across two workers", decisions.count())
	}
}

func TestDifferentConversationsAreDecidedSeparately(t *testing.T) {
	state, decisions, c := newMemoryState(), &recordedDecisions{}, &clock{now: t0}
	coalescer := newCoalescer(t, state, decisions, c)
	a, b := entry(), entry()
	b.EntryID = "other"
	_ = coalescer.Stamp(a)
	_ = coalescer.Stamp(b)
	c.advance(5 * time.Second)
	if n := coalescer.Flush(context.Background()); n != 2 {
		t.Fatalf("decided %d, want 2", n)
	}
}

func TestTheCoalescerNeedsAtomicAcknowledgement(t *testing.T) {
	if _, err := NewCoalescer(struct{ cache.SharedState }{}, func(context.Context, ld.EntryRef) {}, CoalescerConfig{}); err == nil {
		t.Fatal("without compare-and-delete two replicas could decide the same burst")
	}
}

type stampRecorder struct{ stamps []ld.EntryRef }

func (s *stampRecorder) Stamp(ref ld.EntryRef) error { s.stamps = append(s.stamps, ref); return nil }

func TestTheGateQueuesEveryMessageForTheBurstDecision(t *testing.T) {
	h := newHarness()
	stamps := &stampRecorder{}
	gate := NewGate(h.service, stamps)

	gate.Queue(entry())

	if len(stamps.stamps) != 1 || h.model.calls() != 0 {
		t.Fatalf("a message is queued, never decided inline: stamps=%d calls=%d", len(stamps.stamps), h.model.calls())
	}
}

func TestTheGateQueuesNothingWithoutAModelOrAnEntry(t *testing.T) {
	h := newHarness()
	stamps := &stampRecorder{}
	gate := NewGate(h.service, stamps)

	gate.Queue(ld.EntryRef{EntryType: string(shared.EntryTypeWhatsApp)})
	gate.Queue(ld.EntryRef{EntryID: "entry"})
	h.service.deps.Model = nil
	gate.Queue(entry())
	var unwired *Gate
	unwired.Queue(entry())

	if len(stamps.stamps) != 0 {
		t.Fatalf("stamps = %d", len(stamps.stamps))
	}
}

func TestAnUnboundHandleBehavesLikeToday(t *testing.T) {
	handle := &GateHandle{}
	handle.Queue(entry())
	if handle.Acts(context.Background(), "ws") {
		t.Fatal("before binding nothing acts")
	}
	if !handle.StageNeedsReview(context.Background(), quietTarget()) {
		t.Fatal("before binding the LLM keeps the stage")
	}
	if gate := handle.QuietGate(context.Background(), quietTarget(), true, true, "", ""); !gate.NeedsMemory || !gate.NeedsDeals {
		t.Fatal("before binding the LLM keeps memory and opportunities")
	}
}

func TestABoundHandleForwardsToTheGate(t *testing.T) {
	h := newHarness()
	stamps := &stampRecorder{}
	handle := &GateHandle{}
	handle.Bind(NewGate(h.service, stamps))
	handle.Queue(entry())
	if len(stamps.stamps) != 1 || !handle.Acts(context.Background(), "ws") {
		t.Fatal("a bound handle queues through its gate")
	}
}

func TestADeferredDecisionReadsTheConversationSettingsThenDecides(t *testing.T) {
	h := newHarness()
	h.service.DecideLater(context.Background(), entry())
	if h.model.calls() != 1 || len(h.effects.moved) != 1 {
		t.Fatal("a coalesced decision is an ordinary decision")
	}
	if h.subjects.asked[0] != entry() {
		t.Fatalf("settings read for %+v", h.subjects.asked[0])
	}
	h.model.err = decision.ErrUnavailable
	h.service.DecideLater(context.Background(), entry())
}

func TestADeferredDecisionWithBothSwitchesOffAsksNothing(t *testing.T) {
	h := newHarness()
	h.subjects.trigger.Features = ld.Features{}
	h.service.DecideLater(context.Background(), entry())
	if h.model.calls() != 0 {
		t.Fatal("a channel with analysis and auto staging off is never read")
	}
}

func TestADeferredDecisionForAConversationThatIsGoneAsksNothing(t *testing.T) {
	h := newHarness()
	h.subjects.found = false
	h.service.DecideLater(context.Background(), entry())
	if h.model.calls() != 0 {
		t.Fatal("a deleted conversation is not decided")
	}
}

func TestADeferredDecisionWhoseSettingsCannotBeReadDecidesNothing(t *testing.T) {
	h := newHarness()
	h.subjects.err = errors.New("db down")
	h.service.DecideLater(context.Background(), entry())
	if h.model.calls() != 0 || len(h.effects.moved) != 0 {
		t.Fatal("unknown switches must not be read as on")
	}
}

func TestTheGateQueuesNothingWithoutASettingsReader(t *testing.T) {
	h := newHarness()
	h.service.deps.Subjects = nil
	stamps := &stampRecorder{}
	NewGate(h.service, stamps).Queue(entry())
	if len(stamps.stamps) != 0 || h.service.Acts(context.Background(), "ws") {
		t.Fatal("without the conversation settings nothing may be decided")
	}
}
