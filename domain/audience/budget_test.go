package audience

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"vozko/domain/shared"
)

// The budgeter is pure arithmetic and it is the answer to "respects the AI's
// max tokens per call and per cycle". Every bound below closes a batch, and
// every bound is tested in isolation by making the others impossibly loose.

func loose() Budget {
	return Budget{
		MaxBatchItems:        HardMaxBatchItems,
		MaxInputTokens:       1_000_000,
		MaxOutputTokens:      1_000_000,
		PerItemOutputTokens:  1,
		PerItemInputOverhead: 0,
		MaxCommentRunes:      10_000,
		ReserveTokens:        0,
		SafetyFactor:         1.0,
		MaxTokensPerCycle:    100_000_000,
		MaxBatchesPerCycle:   1_000,
	}
}

func items(n int, text string) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{ID: fmt.Sprintf("c-%d", i+1), Text: text}
	}
	return out
}

func totalItems(plans []BatchPlan) int {
	n := 0
	for _, p := range plans {
		n += len(p.Items)
	}
	return n
}

func TestDefaultBudget_IsValid(t *testing.T) {
	b := DefaultBudget()
	if err := b.Validate(); err != nil {
		t.Fatalf("default budget must validate: %v", err)
	}
	// The plan's numbers, pinned: a change here is a product decision.
	if b.MaxBatchItems != 20 || b.MaxInputTokens != 12_000 || b.MaxOutputTokens != 4_000 ||
		b.PerItemOutputTokens != 64 || b.MaxCommentRunes != 600 || b.ReserveTokens != 512 ||
		b.SafetyFactor != 1.35 || b.MaxTokensPerCycle != 120_000 || b.MaxBatchesPerCycle != 12 {
		t.Fatalf("defaults drifted: %+v", b)
	}
}

func TestBudget_NormalizeFillsDefaultsAndClamps(t *testing.T) {
	var b Budget
	b.Normalize()
	if b != DefaultBudget() {
		t.Fatalf("zero budget should normalise to the defaults, got %+v", b)
	}
	b = Budget{MaxBatchItems: 500}
	b.Normalize()
	if b.MaxBatchItems != HardMaxBatchItems {
		t.Fatalf("MaxBatchItems must be clamped to %d, got %d", HardMaxBatchItems, b.MaxBatchItems)
	}
	// A safety factor below 1 would under-estimate, the one direction the
	// budgeter must never lean.
	b = Budget{SafetyFactor: 0.5}
	b.Normalize()
	if b.SafetyFactor < 1 {
		t.Fatalf("SafetyFactor must not go below 1, got %v", b.SafetyFactor)
	}
}

func TestBudget_Validate(t *testing.T) {
	cases := map[string]func(*Budget){
		"one item's output must fit":         func(b *Budget) { b.PerItemOutputTokens = b.MaxOutputTokens + 1 },
		"reserve must leave input room":      func(b *Budget) { b.ReserveTokens = b.MaxInputTokens },
		"a full batch must fit the cycle":    func(b *Budget) { b.MaxTokensPerCycle = 100 },
		"batches per cycle must be positive": func(b *Budget) { b.MaxBatchesPerCycle = -1 },
		"comment runes must be positive":     func(b *Budget) { b.MaxCommentRunes = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b := DefaultBudget()
			mutate(&b)
			if err := b.Validate(); !errors.Is(err, ErrBudgetInvalid) {
				t.Errorf("expected ErrBudgetInvalid, got %v", err)
			}
		})
	}
}

// §7.1: the hard cap handed to the provider is item count × per-item output
// plus the reserve, so a runaway generation costs a bounded amount.
func TestBudget_MaxTokensForBatch(t *testing.T) {
	b := DefaultBudget()
	if got := b.MaxTokensForBatch(20); got != 20*64+512 {
		t.Fatalf("MaxTokensForBatch(20) = %d", got)
	}
}

// ---- Bound 1: item count ----

func TestPlanBatches_ClosesOnItemCount(t *testing.T) {
	b := loose()
	b.MaxBatchItems = 7
	plans, rem := PlanBatches(items(20, "oi"), 100, b)
	if len(rem.Unplanned) != 0 {
		t.Fatalf("nothing should be deferred: %+v", rem)
	}
	if len(plans) != 3 {
		t.Fatalf("expected 3 batches (7+7+6), got %d", len(plans))
	}
	if n := len(plans[0].Items); n != 7 {
		t.Errorf("first batch has %d items, want 7", n)
	}
	if n := len(plans[2].Items); n != 6 {
		t.Errorf("last batch has %d items, want 6", n)
	}
	if totalItems(plans) != 20 {
		t.Errorf("items lost or duplicated: %d", totalItems(plans))
	}
}

// ---- Bound 2: input tokens ----

func TestPlanBatches_ClosesOnInputTokens(t *testing.T) {
	b := loose()
	// 40-byte comments are 10 tokens each. With a 100-token system prompt and
	// a 150-token ceiling, exactly 5 fit: 100 + 5×10 = 150; a 6th would be 160.
	b.MaxInputTokens = 150
	text := strings.Repeat("a", 40)
	plans, rem := PlanBatches(items(12, text), 100, b)
	if len(rem.Unplanned) != 0 {
		t.Fatalf("nothing should be deferred: %+v", rem)
	}
	if len(plans) != 3 {
		t.Fatalf("expected 3 batches (5+5+2), got %d", len(plans))
	}
	for i, p := range plans[:2] {
		if len(p.Items) != 5 {
			t.Errorf("batch %d has %d items, want 5", i, len(p.Items))
		}
		if p.EstimatedInputTokens() > b.MaxInputTokens {
			t.Errorf("batch %d estimates %d input tokens, over the %d ceiling", i, p.EstimatedInputTokens(), b.MaxInputTokens)
		}
	}
}

// The safety factor and the reserve both count against the ceiling: the
// estimate handed to the ceiling is ceil((sys + Σ items) × factor) + reserve.
func TestPlanBatches_InputBoundIncludesSafetyAndReserve(t *testing.T) {
	b := loose()
	b.SafetyFactor = 1.35
	b.ReserveTokens = 50
	b.MaxInputTokens = 300
	text := strings.Repeat("a", 40) // 10 tokens
	// ceil((100 + n×10) × 1.35) + 50 ≤ 300  →  (100 + 10n) × 1.35 ≤ 250 → 10n ≤ 85.2 → n ≤ 8
	plans, _ := PlanBatches(items(20, text), 100, b)
	if n := len(plans[0].Items); n != 8 {
		t.Fatalf("first batch has %d items, want 8", n)
	}
	want := int(math.Ceil(float64(100+8*10)*1.35)) + 50
	if got := plans[0].EstimatedInputTokens(); got != want {
		t.Fatalf("EstimatedInputTokens() = %d, want %d", got, want)
	}
}

// The item's own JSON envelope costs tokens too; PerItemInputOverhead is
// charged per item so a batch of 40 one-word comments is not estimated as
// 40 tokens.
func TestPlanBatches_ChargesPerItemOverhead(t *testing.T) {
	b := loose()
	b.PerItemInputOverhead = 8
	b.MaxInputTokens = 100
	// 100 sys? No: sys 0, each item 1 token + 8 overhead = 9. 11 fit (99).
	plans, _ := PlanBatches(items(30, "a"), 0, b)
	if n := len(plans[0].Items); n != 11 {
		t.Fatalf("first batch has %d items, want 11", n)
	}
}

// ---- Bound 3: output tokens ----

func TestPlanBatches_ClosesOnOutputTokens(t *testing.T) {
	b := loose()
	b.PerItemOutputTokens = 64
	b.MaxOutputTokens = 64*6 + 10 // six fit, a seventh does not
	plans, _ := PlanBatches(items(20, "oi"), 100, b)
	if n := len(plans[0].Items); n != 6 {
		t.Fatalf("first batch has %d items, want 6", n)
	}
	if got := plans[0].MaxOutputTokens(); got != 6*64+b.ReserveTokens {
		t.Fatalf("MaxOutputTokens() = %d", got)
	}
}

// ---- Never empty, never infinite ----

// A single item that alone exceeds MaxInputTokens still gets a one-item
// batch. Refusing it would leave it pending forever: an infinite loop across
// ticks instead of within one.
func TestPlanBatches_OversizedItemStillYieldsOneItemBatch(t *testing.T) {
	b := loose()
	b.MaxInputTokens = 50
	huge := strings.Repeat("a", 4000) // 1000 tokens
	plans, rem := PlanBatches([]Item{{ID: "big", Text: huge}, {ID: "small", Text: "oi"}}, 10, b)
	if len(rem.Unplanned) != 0 {
		t.Fatalf("nothing should be deferred: %+v", rem)
	}
	if len(plans) != 2 {
		t.Fatalf("expected the oversized item alone then the small one, got %d batches", len(plans))
	}
	if len(plans[0].Items) != 1 || plans[0].Items[0].ID != "big" {
		t.Fatalf("first batch should be the oversized item alone: %+v", plans[0].Items)
	}
}

func TestPlanBatches_EmptyInput(t *testing.T) {
	plans, rem := PlanBatches(nil, 100, loose())
	if len(plans) != 0 || len(rem.Unplanned) != 0 || rem.Reason != "" {
		t.Fatalf("empty input should plan nothing: %+v %+v", plans, rem)
	}
}

// ---- Truncation ----

// Comments are cut at MaxCommentRunes (RUNES, so a cut never lands inside a
// multi-byte character) and the cut is recorded on the item.
func TestPlanBatches_TruncatesAtRuneBoundary(t *testing.T) {
	b := loose()
	b.MaxCommentRunes = 10
	text := strings.Repeat("ção", 10) // 30 runes, 50 bytes
	plans, _ := PlanBatches(items(1, text), 0, b)
	it := plans[0].Items[0]
	if n := len([]rune(it.Text)); n != 10 {
		t.Fatalf("truncated to %d runes, want 10", n)
	}
	if !it.Truncated {
		t.Fatal("cut must be recorded")
	}
	// Every rune must still be a valid character: a byte cut would leave a
	// replacement char at the end.
	if strings.ContainsRune(it.Text, '�') {
		t.Fatal("truncation split a character")
	}
	short, _ := PlanBatches(items(1, "curto"), 0, b)
	if short[0].Items[0].Truncated {
		t.Fatal("an untouched comment must not be flagged")
	}
}

// Token estimation runs on the TRUNCATED text: the model never sees the
// tail, so the tail must not count against the batch.
func TestPlanBatches_EstimatesTruncatedText(t *testing.T) {
	b := loose()
	b.MaxCommentRunes = 40
	long := strings.Repeat("a", 4000)
	plans, _ := PlanBatches(items(1, long), 0, b)
	if got := plans[0].Items[0].Tokens; got != shared.EstimateTokens(strings.Repeat("a", 40)) {
		t.Fatalf("item tokens = %d, want the truncated estimate", got)
	}
}

// ---- Refs ----

// Refs are batch-local, 1..N, in item order. The model returns the ref, not
// the text, and the use case reconciles by it, so it must be dense and
// deterministic.
func TestPlanBatches_RefsAreDenseAndBatchLocal(t *testing.T) {
	b := loose()
	b.MaxBatchItems = 3
	plans, _ := PlanBatches(items(7, "oi"), 0, b)
	for pi, p := range plans {
		for i, it := range p.Items {
			if it.Ref != i+1 {
				t.Errorf("batch %d item %d has ref %d, want %d", pi, i, it.Ref, i+1)
			}
		}
	}
	if plans[1].Items[0].ID != "c-4" {
		t.Errorf("item order not preserved across batches: %+v", plans[1].Items)
	}
}

// ---- Cycle ceiling ----

// The per-cycle ceiling DEFERS rather than drops: unplanned items are handed
// back so the caller leaves them pending for the next tick. This is the
// literal reading of "respects the max tokens per cycle".
func TestPlanBatches_CycleTokenCeilingDefers(t *testing.T) {
	b := loose()
	b.MaxBatchItems = 5
	b.PerItemOutputTokens = 10
	// Each 5-item batch: input 0 + 5×1 = 5, output 5×10 = 50 → 55 tokens.
	// Ceiling 120 → two batches (110); a third (165) would cross.
	b.MaxTokensPerCycle = 120
	plans, rem := PlanBatches(items(17, "a"), 0, b)
	if len(plans) != 2 {
		t.Fatalf("expected 2 batches under the cycle ceiling, got %d", len(plans))
	}
	if len(rem.Unplanned) != 7 {
		t.Fatalf("expected 7 deferred items, got %d", len(rem.Unplanned))
	}
	if rem.Reason != RemainderCycleTokens {
		t.Fatalf("reason = %q, want %q", rem.Reason, RemainderCycleTokens)
	}
	if rem.Unplanned[0].ID != "c-11" {
		t.Fatalf("deferred items must be the tail in order, got %s first", rem.Unplanned[0].ID)
	}
	if totalItems(plans)+len(rem.Unplanned) != 17 {
		t.Fatal("items lost")
	}
}

func TestPlanBatches_CycleBatchCeilingDefers(t *testing.T) {
	b := loose()
	b.MaxBatchItems = 4
	b.MaxBatchesPerCycle = 2
	plans, rem := PlanBatches(items(10, "a"), 0, b)
	if len(plans) != 2 || len(rem.Unplanned) != 2 {
		t.Fatalf("expected 2 batches and 2 deferred, got %d/%d", len(plans), len(rem.Unplanned))
	}
	if rem.Reason != RemainderCycleBatches {
		t.Fatalf("reason = %q", rem.Reason)
	}
}

// A partial batch at the very end that would cross the ceiling is deferred
// whole, not trimmed: the next tick has a fresh ceiling and will take it.
func TestPlanBatches_CycleCeilingChecksWholeBatch(t *testing.T) {
	b := loose()
	b.MaxBatchItems = 5
	b.PerItemOutputTokens = 10
	// The full batch (55) fits. The 3-item tail (33) would cross at 88, even
	// though two of its items (22) would squeeze in: it is deferred whole.
	b.MaxTokensPerCycle = 80
	plans, rem := PlanBatches(items(8, "a"), 0, b)
	if len(plans) != 1 || len(plans[0].Items) != 5 {
		t.Fatalf("expected exactly one full batch, got %+v", plans)
	}
	if len(rem.Unplanned) != 3 {
		t.Fatalf("expected 3 deferred, got %d", len(rem.Unplanned))
	}
}

// The flush job plans several containers in one tick against ONE workspace
// ceiling. WithCycleAllowance narrows the ceiling to what is left.
func TestBudget_WithCycleAllowance(t *testing.T) {
	b := loose()
	b.MaxTokensPerCycle = 1000
	if got := b.WithCycleAllowance(400).MaxTokensPerCycle; got != 400 {
		t.Fatalf("allowance 400 → %d", got)
	}
	if got := b.WithCycleAllowance(5000).MaxTokensPerCycle; got != 1000 {
		t.Fatalf("allowance above the ceiling must not raise it: %d", got)
	}
	if got := b.WithCycleAllowance(-1).MaxTokensPerCycle; got != 0 {
		t.Fatalf("negative allowance is zero: %d", got)
	}
	// Zero allowance plans nothing and defers everything.
	plans, rem := PlanBatches(items(3, "a"), 0, b.WithCycleAllowance(0))
	if len(plans) != 0 || len(rem.Unplanned) != 3 || rem.Reason != RemainderCycleTokens {
		t.Fatalf("zero allowance: %+v %+v", plans, rem)
	}
}

func TestBatchPlan_EstimatedTotalTokens(t *testing.T) {
	b := loose()
	b.PerItemOutputTokens = 10
	b.ReserveTokens = 5
	plans, _ := PlanBatches(items(2, strings.Repeat("a", 40)), 100, b) // 2×10 + 100 = 120 in
	p := plans[0]
	if p.EstimatedInputTokens() != 125 { // ×1.0 + reserve 5
		t.Errorf("input = %d", p.EstimatedInputTokens())
	}
	if p.MaxOutputTokens() != 25 { // 2×10 + 5
		t.Errorf("output = %d", p.MaxOutputTokens())
	}
	if p.EstimatedTotalTokens() != 150 {
		t.Errorf("total = %d", p.EstimatedTotalTokens())
	}
}

// ---- Split (the FinishReason=="length" path, §7.2) ----

func TestBatchPlan_Split(t *testing.T) {
	b := loose()
	plans, _ := PlanBatches(items(7, "oi"), 100, b)
	halves := plans[0].Split()
	if len(halves) != 2 {
		t.Fatalf("expected 2 halves, got %d", len(halves))
	}
	if len(halves[0].Items) != 4 || len(halves[1].Items) != 3 {
		t.Fatalf("expected 4+3, got %d+%d", len(halves[0].Items), len(halves[1].Items))
	}
	// Refs are renumbered per half: the model sees a fresh 1..N.
	for _, h := range halves {
		for i, it := range h.Items {
			if it.Ref != i+1 {
				t.Errorf("ref %d at index %d after split", it.Ref, i)
			}
		}
		if h.MaxOutputTokens() != b.MaxTokensForBatch(len(h.Items)) {
			t.Errorf("half's output cap not recomputed")
		}
	}
	if halves[1].Items[0].ID != "c-5" {
		t.Errorf("order not preserved: %+v", halves[1].Items)
	}
}

func TestBatchPlan_SplitSingleIsItself(t *testing.T) {
	plans, _ := PlanBatches(items(1, "oi"), 0, loose())
	halves := plans[0].Split()
	if len(halves) != 1 || len(halves[0].Items) != 1 {
		t.Fatalf("a one-item batch cannot be split: %+v", halves)
	}
}

func TestBatchPlan_IDs(t *testing.T) {
	plans, _ := PlanBatches(items(3, "oi"), 0, loose())
	ids := plans[0].IDs()
	if len(ids) != 3 || ids[0] != "c-1" || ids[2] != "c-3" {
		t.Fatalf("IDs() = %v", ids)
	}
}
