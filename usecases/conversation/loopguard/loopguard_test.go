package loopguard

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/cache"
)

type fakeSharedState struct {
	mu      sync.Mutex
	counts  map[string]int64
	failErr error
	calls   int64
}

func newFakeSharedState() *fakeSharedState {
	return &fakeSharedState{counts: map[string]int64{}}
}

func (f *fakeSharedState) IncrWithTTL(key string, _ time.Duration) (int64, error) {
	atomic.AddInt64(&f.calls, 1)
	if f.failErr != nil {
		return 0, f.failErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[key]++
	return f.counts[key], nil
}

func (f *fakeSharedState) reset(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.counts, key)
}

func (f *fakeSharedState) SetNX(string, string, time.Duration) (bool, error) {
	panic("loopguard test: unexpected SetNX call")
}
func (f *fakeSharedState) SetString(string, string, time.Duration) error {
	panic("loopguard test: unexpected SetString call")
}
func (f *fakeSharedState) GetString(string) (string, error) {
	panic("loopguard test: unexpected GetString call")
}
func (f *fakeSharedState) Del(...string) error {
	panic("loopguard test: unexpected Del call")
}
func (f *fakeSharedState) Exists(string) (bool, error) {
	panic("loopguard test: unexpected Exists call")
}
func (f *fakeSharedState) Incr(string) (int64, error) {
	panic("loopguard test: unexpected Incr call")
}
func (f *fakeSharedState) Decr(string) (int64, error) {
	panic("loopguard test: unexpected Decr call")
}
func (f *fakeSharedState) TryIncr(string, int64) (bool, error) {
	panic("loopguard test: unexpected TryIncr call")
}
func (f *fakeSharedState) SAdd(string, ...string) error {
	panic("loopguard test: unexpected SAdd call")
}
func (f *fakeSharedState) SRem(string, ...string) error {
	panic("loopguard test: unexpected SRem call")
}
func (f *fakeSharedState) SMembers(string) ([]string, error) {
	panic("loopguard test: unexpected SMembers call")
}
func (f *fakeSharedState) Publish(string, []byte) error {
	panic("loopguard test: unexpected Publish call")
}
func (f *fakeSharedState) Subscribe(context.Context, string, func([]byte)) {
	panic("loopguard test: unexpected Subscribe call")
}
func (f *fakeSharedState) HSet(string, string, string) error {
	panic("loopguard test: unexpected HSet call")
}
func (f *fakeSharedState) HDel(string, string) error {
	panic("loopguard test: unexpected HDel call")
}
func (f *fakeSharedState) HGetAll(string) (map[string]string, error) {
	panic("loopguard test: unexpected HGetAll call")
}
func (f *fakeSharedState) HIncrBy(string, string, int64) (int64, error) {
	panic("loopguard test: unexpected HIncrBy call")
}
func (f *fakeSharedState) IncrBy(string, int64) (int64, error) {
	panic("loopguard test: unexpected IncrBy call")
}
func (f *fakeSharedState) DecrBy(string, int64) (int64, error) {
	panic("loopguard test: unexpected DecrBy call")
}
func (f *fakeSharedState) TryIncrBy(string, int64, int64) (bool, error) {
	panic("loopguard test: unexpected TryIncrBy call")
}
func (f *fakeSharedState) Expire(string, time.Duration) (bool, error) {
	panic("loopguard test: unexpected Expire call")
}

var _ cache.SharedState = (*fakeSharedState)(nil)

type fakeMetrics struct {
	mu       sync.Mutex
	checked  map[string]int
	blocked  map[string]int
	checkSum int
	blockSum int
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{checked: map[string]int{}, blocked: map[string]int{}}
}

func (m *fakeMetrics) LoopGuardChecked(layer, action string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checked[layer+"|"+action]++
	m.checkSum++
}

func (m *fakeMetrics) LoopGuardBlocked(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocked[reason]++
	m.blockSum++
}

func (m *fakeMetrics) totalBlocks() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.blockSum
}

const (
	tWs   = "ws-1"
	tConv = "entry-abc"
)

func newGuardForTest() (Guard, *fakeSharedState, *fakeMetrics) {
	st := newFakeSharedState()
	mx := newFakeMetrics()
	g := NewGuard(st, Config{
		DuplicateWindow:    10 * time.Minute,
		DuplicateThreshold: 3,
		AIRateWindow:       5 * time.Minute,
		AIRateMax:          4,
		KeyPrefix:          "lg_test",
	}, mx)
	return g, st, mx
}

func TestCheckInbound_AllowsBelowThreshold(t *testing.T) {
	t.Parallel()
	g, _, mx := newGuardForTest()
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		dec := g.CheckInbound(ctx, tWs, tConv, "ola tudo bem")
		if dec.Block {
			t.Fatalf("occurrence %d should not block, got %+v", i, dec)
		}
		if dec.Count != int64(i) {
			t.Fatalf("occurrence %d expected count=%d, got %d", i, i, dec.Count)
		}
	}
	if mx.totalBlocks() != 0 {
		t.Fatalf("expected zero blocks, got %d", mx.totalBlocks())
	}
}

func TestCheckInbound_BlocksAtAndAboveThreshold(t *testing.T) {
	t.Parallel()
	g, _, mx := newGuardForTest()
	ctx := context.Background()

	_ = g.CheckInbound(ctx, tWs, tConv, "ola")
	_ = g.CheckInbound(ctx, tWs, tConv, "ola")

	for i := 3; i <= 5; i++ {
		dec := g.CheckInbound(ctx, tWs, tConv, "ola")
		if !dec.Block {
			t.Fatalf("occurrence %d expected Block=true, got %+v", i, dec)
		}
		if dec.Reason != ReasonDuplicateInbound {
			t.Fatalf("expected reason %q, got %q", ReasonDuplicateInbound, dec.Reason)
		}
	}
	if mx.totalBlocks() != 3 {
		t.Fatalf("expected 3 blocks recorded, got %d", mx.totalBlocks())
	}
}

func TestCheckInbound_NormalizationGroupsVariants(t *testing.T) {
	t.Parallel()
	g, _, _ := newGuardForTest()
	ctx := context.Background()

	variants := []string{
		"Olá, tudo bem?",
		"OLÁ tudo bem!!!",
		"   olá   tudo   bem   ",
	}
	for i, v := range variants {
		dec := g.CheckInbound(ctx, tWs, tConv, v)
		if i < 2 && dec.Block {
			t.Fatalf("variant %d should not block yet: %+v", i, dec)
		}
		if i == 2 && !dec.Block {
			t.Fatalf("variant %d should block (3rd same fingerprint): %+v", i, dec)
		}
	}
}

func TestCheckInbound_DifferentConversationsAreIsolated(t *testing.T) {
	t.Parallel()
	g, _, _ := newGuardForTest()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		dec := g.CheckInbound(ctx, tWs, "conv-A", "spam text")
		if dec.Block && i < 2 {
			t.Fatalf("conv-A early: %+v", dec)
		}
		dec = g.CheckInbound(ctx, tWs, "conv-B", "spam text")
		if dec.Block && i < 2 {
			t.Fatalf("conv-B early: %+v", dec)
		}
	}
}

func TestCheckInbound_DifferentWorkspacesAreIsolated(t *testing.T) {
	t.Parallel()
	g, _, _ := newGuardForTest()
	ctx := context.Background()

	_ = g.CheckInbound(ctx, "ws-A", tConv, "x")
	_ = g.CheckInbound(ctx, "ws-A", tConv, "x")
	dec := g.CheckInbound(ctx, "ws-B", tConv, "x")
	if dec.Block {
		t.Fatalf("ws-B first occurrence must not block: %+v", dec)
	}
}

func TestCheckInbound_DifferentFingerprintsDoNotInterfere(t *testing.T) {
	t.Parallel()
	g, _, _ := newGuardForTest()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		dec := g.CheckInbound(ctx, tWs, tConv, "primeiro tipo de mensagem")
		if dec.Block && i < 2 {
			t.Fatalf("first family early block: %+v", dec)
		}
	}
	dec := g.CheckInbound(ctx, tWs, tConv, "outra mensagem totalmente distinta")
	if dec.Block {
		t.Fatalf("distinct fingerprint must not inherit the other counter: %+v", dec)
	}
	if dec.Count != 1 {
		t.Fatalf("distinct fingerprint must start at 1, got %d", dec.Count)
	}
}

func TestCheckInbound_EmptyConversationIDIsAllowed(t *testing.T) {
	t.Parallel()
	g, st, _ := newGuardForTest()
	ctx := context.Background()

	dec := g.CheckInbound(ctx, tWs, "  ", "anything")
	if dec.Block {
		t.Fatalf("empty conversation must not block: %+v", dec)
	}
	if st.calls != 0 {
		t.Fatalf("expected no backend calls when conv-id is empty, got %d", st.calls)
	}
}

func TestCheckInbound_EmptyTextIsAllowed(t *testing.T) {
	t.Parallel()
	g, st, _ := newGuardForTest()
	ctx := context.Background()

	dec := g.CheckInbound(ctx, tWs, tConv, "    \t\n  ")
	if dec.Block {
		t.Fatalf("whitespace-only text must not block: %+v", dec)
	}
	if st.calls != 0 {
		t.Fatalf("expected no backend calls for unfingerprintable text, got %d", st.calls)
	}
}

func TestCheckInbound_FailsOpenOnBackendError(t *testing.T) {
	t.Parallel()
	g, st, mx := newGuardForTest()
	ctx := context.Background()
	st.failErr = errors.New("redis: connection refused")

	for i := 0; i < 5; i++ {
		dec := g.CheckInbound(ctx, tWs, tConv, "any message")
		if dec.Block {
			t.Fatalf("expected fail-open on backend error, got block: %+v", dec)
		}
	}
	if mx.totalBlocks() != 0 {
		t.Fatalf("expected no blocks recorded on backend error, got %d", mx.totalBlocks())
	}
}

func TestRecordAIResponse_AllowsUpToMaxThenBlocks(t *testing.T) {
	t.Parallel()
	g, _, mx := newGuardForTest()
	ctx := context.Background()

	for i := 1; i <= 4; i++ {
		dec := g.RecordAIResponse(ctx, tWs, tConv)
		if dec.Block {
			t.Fatalf("reply %d should pass, got block: %+v", i, dec)
		}
	}
	for i := 5; i <= 7; i++ {
		dec := g.RecordAIResponse(ctx, tWs, tConv)
		if !dec.Block {
			t.Fatalf("reply %d should block, got %+v", i, dec)
		}
		if dec.Reason != ReasonAIRateLimit {
			t.Fatalf("expected reason %q, got %q", ReasonAIRateLimit, dec.Reason)
		}
	}
	if mx.totalBlocks() != 3 {
		t.Fatalf("expected 3 airate blocks, got %d", mx.totalBlocks())
	}
}

func TestRecordAIResponse_EmptyConversationIsNoop(t *testing.T) {
	t.Parallel()
	g, st, _ := newGuardForTest()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		dec := g.RecordAIResponse(ctx, tWs, "")
		if dec.Block {
			t.Fatalf("empty conv must not block: %+v", dec)
		}
	}
	if st.calls != 0 {
		t.Fatalf("expected no backend calls for empty conv, got %d", st.calls)
	}
}

func TestRecordAIResponse_FailsOpenOnBackendError(t *testing.T) {
	t.Parallel()
	g, st, _ := newGuardForTest()
	ctx := context.Background()
	st.failErr = errors.New("boom")

	for i := 0; i < 100; i++ {
		dec := g.RecordAIResponse(ctx, tWs, tConv)
		if dec.Block {
			t.Fatalf("must fail open on backend error, got block at i=%d", i)
		}
	}
}

func TestNewGuard_NilStateReturnsAlwaysAllow(t *testing.T) {
	t.Parallel()
	g := NewGuard(nil, Config{}, nil)
	ctx := context.Background()

	for i := 0; i < 1000; i++ {
		if dec := g.CheckInbound(ctx, tWs, tConv, "anything"); dec.Block {
			t.Fatalf("AlwaysAllow must never block (CheckInbound)")
		}
		if dec := g.RecordAIResponse(ctx, tWs, tConv); dec.Block {
			t.Fatalf("AlwaysAllow must never block (RecordAIResponse)")
		}
	}
}

func TestConfig_DefaultsAreSane(t *testing.T) {
	t.Parallel()
	d := DefaultConfig()
	if d.DuplicateWindow < time.Minute {
		t.Fatalf("DuplicateWindow too small: %v", d.DuplicateWindow)
	}
	if d.DuplicateThreshold < 2 {
		t.Fatalf("DuplicateThreshold must be ≥ 2 to allow legitimate retries: %d", d.DuplicateThreshold)
	}
	if d.AIRateMax < 2 {
		t.Fatalf("AIRateMax too restrictive: %d", d.AIRateMax)
	}
	if d.AIRateWindow < time.Minute {
		t.Fatalf("AIRateWindow too small: %v", d.AIRateWindow)
	}
	if d.KeyPrefix == "" {
		t.Fatalf("KeyPrefix must default to a non-empty value")
	}
}

func TestConfig_ResolveFillsZeroFields(t *testing.T) {
	t.Parallel()
	r := Config{}.resolve()
	d := DefaultConfig()
	if r != d {
		t.Fatalf("zero Config should resolve to defaults; got %+v vs %+v", r, d)
	}

	custom := Config{DuplicateThreshold: 7}.resolve()
	if custom.DuplicateThreshold != 7 {
		t.Fatalf("custom value lost: %+v", custom)
	}
	if custom.DuplicateWindow != d.DuplicateWindow {
		t.Fatalf("other fields should be defaulted: %+v", custom)
	}
}

func TestMetrics_PassAndBlockAreLabeled(t *testing.T) {
	t.Parallel()
	g, _, mx := newGuardForTest()
	ctx := context.Background()

	g.CheckInbound(ctx, tWs, tConv, "spam")
	g.CheckInbound(ctx, tWs, tConv, "spam")
	g.CheckInbound(ctx, tWs, tConv, "spam")

	if mx.checked["inbound|pass"] != 2 {
		t.Fatalf("expected 2 inbound passes, got %d", mx.checked["inbound|pass"])
	}
	if mx.checked["inbound|block"] != 1 {
		t.Fatalf("expected 1 inbound block, got %d", mx.checked["inbound|block"])
	}
	if mx.blocked[ReasonDuplicateInbound] != 1 {
		t.Fatalf("expected 1 duplicate_inbound block, got %d", mx.blocked[ReasonDuplicateInbound])
	}

	for i := 0; i < 5; i++ {
		g.RecordAIResponse(ctx, tWs, "other-conv")
	}
	if mx.checked["airate|pass"] != 4 {
		t.Fatalf("expected 4 airate passes, got %d", mx.checked["airate|pass"])
	}
	if mx.checked["airate|block"] != 1 {
		t.Fatalf("expected 1 airate block, got %d", mx.checked["airate|block"])
	}
}

func TestKeyPrefixIsApplied(t *testing.T) {
	t.Parallel()
	st := newFakeSharedState()
	g := NewGuard(st, Config{
		DuplicateWindow:    time.Minute,
		DuplicateThreshold: 3,
		AIRateWindow:       time.Minute,
		AIRateMax:          5,
		KeyPrefix:          "customprefix",
	}, NoopMetrics{})

	g.CheckInbound(context.Background(), tWs, tConv, "hello")
	st.mu.Lock()
	defer st.mu.Unlock()
	for k := range st.counts {
		if k == "" {
			t.Fatal("empty key")
		}
		if got := k[:len("customprefix:")]; got != "customprefix:" {
			t.Fatalf("expected key prefix to be applied, got key %q", k)
		}
	}
}
