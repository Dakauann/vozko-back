package audience

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"vozko/domain/shared"
)

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

func TestBudget_MaxTokensForBatch(t *testing.T) {
	b := DefaultBudget()
	if got := b.MaxTokensForBatch(20); got != 20*64+512 {
		t.Fatalf("MaxTokensForBatch(20) = %d", got)
	}
}

func TestPlanBatches_ClosesOnItemCount(t *testing.T) {
	b := loose()
	b.MaxBatchItems = 7
	plans, rem := PlanBatches(items(20, "oi"), 100, b, SubjectKindComment)
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

func TestPlanBatches_ClosesOnInputTokens(t *testing.T) {
	b := loose()
	b.MaxInputTokens = 150
	text := strings.Repeat("a", 40)
	plans, rem := PlanBatches(items(12, text), 100, b, SubjectKindComment)
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

func TestPlanBatches_InputBoundIncludesSafetyAndReserve(t *testing.T) {
	b := loose()
	b.SafetyFactor = 1.35
	b.ReserveTokens = 50
	b.MaxInputTokens = 300
	text := strings.Repeat("a", 40)
	plans, _ := PlanBatches(items(20, text), 100, b, SubjectKindComment)
	if n := len(plans[0].Items); n != 8 {
		t.Fatalf("first batch has %d items, want 8", n)
	}
	want := int(math.Ceil(float64(100+8*10)*1.35)) + 50
	if got := plans[0].EstimatedInputTokens(); got != want {
		t.Fatalf("EstimatedInputTokens() = %d, want %d", got, want)
	}
}

func TestPlanBatches_ChargesPerItemOverhead(t *testing.T) {
	b := loose()
	b.PerItemInputOverhead = 8
	b.MaxInputTokens = 100
	plans, _ := PlanBatches(items(30, "a"), 0, b, SubjectKindComment)
	if n := len(plans[0].Items); n != 11 {
		t.Fatalf("first batch has %d items, want 11", n)
	}
}

func TestPlanBatches_ClosesOnOutputTokens(t *testing.T) {
	b := loose()
	b.PerItemOutputTokens = 64
	b.MaxOutputTokens = 64*6 + 10
	plans, _ := PlanBatches(items(20, "oi"), 100, b, SubjectKindComment)
	if n := len(plans[0].Items); n != 6 {
		t.Fatalf("first batch has %d items, want 6", n)
	}
	if got := plans[0].MaxOutputTokens(); got != 6*64+b.ReserveTokens {
		t.Fatalf("MaxOutputTokens() = %d", got)
	}
}

func TestPlanBatches_OversizedItemStillYieldsOneItemBatch(t *testing.T) {
	b := loose()
	b.MaxInputTokens = 50
	huge := strings.Repeat("a", 4000)
	plans, rem := PlanBatches([]Item{{ID: "big", Text: huge}, {ID: "small", Text: "oi"}}, 10, b, SubjectKindComment)
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
	plans, rem := PlanBatches(nil, 100, loose(), SubjectKindComment)
	if len(plans) != 0 || len(rem.Unplanned) != 0 || rem.Reason != "" {
		t.Fatalf("empty input should plan nothing: %+v %+v", plans, rem)
	}
}

func TestPlanBatches_TruncatesAtRuneBoundary(t *testing.T) {
	b := loose()
	b.MaxCommentRunes = 10
	text := strings.Repeat("ção", 10)
	plans, _ := PlanBatches(items(1, text), 0, b, SubjectKindComment)
	it := plans[0].Items[0]
	if n := len([]rune(it.Text)); n != 10 {
		t.Fatalf("truncated to %d runes, want 10", n)
	}
	if !it.Truncated {
		t.Fatal("cut must be recorded")
	}
	if strings.ContainsRune(it.Text, '�') {
		t.Fatal("truncation split a character")
	}
	short, _ := PlanBatches(items(1, "curto"), 0, b, SubjectKindComment)
	if short[0].Items[0].Truncated {
		t.Fatal("an untouched comment must not be flagged")
	}
}

func TestPlanBatches_EstimatesTruncatedText(t *testing.T) {
	b := loose()
	b.MaxCommentRunes = 40
	long := strings.Repeat("a", 4000)
	plans, _ := PlanBatches(items(1, long), 0, b, SubjectKindComment)
	if got := plans[0].Items[0].Tokens; got != shared.EstimateTokens(strings.Repeat("a", 40)) {
		t.Fatalf("item tokens = %d, want the truncated estimate", got)
	}
}

func TestPlanBatches_RefsAreDenseAndBatchLocal(t *testing.T) {
	b := loose()
	b.MaxBatchItems = 3
	plans, _ := PlanBatches(items(7, "oi"), 0, b, SubjectKindComment)
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

func TestPlanBatches_CycleTokenCeilingDefers(t *testing.T) {
	b := loose()
	b.MaxBatchItems = 5
	b.PerItemOutputTokens = 10
	b.MaxTokensPerCycle = 120
	plans, rem := PlanBatches(items(17, "a"), 0, b, SubjectKindComment)
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
	plans, rem := PlanBatches(items(10, "a"), 0, b, SubjectKindComment)
	if len(plans) != 2 || len(rem.Unplanned) != 2 {
		t.Fatalf("expected 2 batches and 2 deferred, got %d/%d", len(plans), len(rem.Unplanned))
	}
	if rem.Reason != RemainderCycleBatches {
		t.Fatalf("reason = %q", rem.Reason)
	}
}

func TestPlanBatches_CycleCeilingChecksWholeBatch(t *testing.T) {
	b := loose()
	b.MaxBatchItems = 5
	b.PerItemOutputTokens = 10
	b.MaxTokensPerCycle = 80
	plans, rem := PlanBatches(items(8, "a"), 0, b, SubjectKindComment)
	if len(plans) != 1 || len(plans[0].Items) != 5 {
		t.Fatalf("expected exactly one full batch, got %+v", plans)
	}
	if len(rem.Unplanned) != 3 {
		t.Fatalf("expected 3 deferred, got %d", len(rem.Unplanned))
	}
}

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
	plans, rem := PlanBatches(items(3, "a"), 0, b.WithCycleAllowance(0), SubjectKindComment)
	if len(plans) != 0 || len(rem.Unplanned) != 3 || rem.Reason != RemainderCycleTokens {
		t.Fatalf("zero allowance: %+v %+v", plans, rem)
	}
}

func TestBatchPlan_EstimatedTotalTokens(t *testing.T) {
	b := loose()
	b.PerItemOutputTokens = 10
	b.ReserveTokens = 5
	plans, _ := PlanBatches(items(2, strings.Repeat("a", 40)), 100, b, SubjectKindComment)
	p := plans[0]
	if p.EstimatedInputTokens() != 125 {
		t.Errorf("input = %d", p.EstimatedInputTokens())
	}
	if p.MaxOutputTokens() != 25 {
		t.Errorf("output = %d", p.MaxOutputTokens())
	}
	if p.EstimatedTotalTokens() != 150 {
		t.Errorf("total = %d", p.EstimatedTotalTokens())
	}
}

func TestBatchPlan_Split(t *testing.T) {
	b := loose()
	plans, _ := PlanBatches(items(7, "oi"), 100, b, SubjectKindComment)
	halves := plans[0].Split()
	if len(halves) != 2 {
		t.Fatalf("expected 2 halves, got %d", len(halves))
	}
	if len(halves[0].Items) != 4 || len(halves[1].Items) != 3 {
		t.Fatalf("expected 4+3, got %d+%d", len(halves[0].Items), len(halves[1].Items))
	}
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
	plans, _ := PlanBatches(items(1, "oi"), 0, loose(), SubjectKindComment)
	halves := plans[0].Split()
	if len(halves) != 1 || len(halves[0].Items) != 1 {
		t.Fatalf("a one-item batch cannot be split: %+v", halves)
	}
}

func TestBatchPlan_IDs(t *testing.T) {
	plans, _ := PlanBatches(items(3, "oi"), 0, loose(), SubjectKindComment)
	ids := plans[0].IDs()
	if len(ids) != 3 || ids[0] != "c-1" || ids[2] != "c-3" {
		t.Fatalf("IDs() = %v", ids)
	}
}

func TestPlanBatchesGivesAConversationATranscriptSizedCap(t *testing.T) {
	b := loose()
	b.MaxCommentRunes = 600
	b.MaxTranscriptRunes = 20_000
	b.Normalize()

	transcript := strings.Repeat("User: preciso de um orcamento para 200 unidades\n", 60)
	if len([]rune(transcript)) <= b.MaxCommentRunes {
		t.Fatalf("the fixture must be longer than the comment cap, got %d runes", len([]rune(transcript)))
	}

	plans, _ := PlanBatches([]Item{{ID: "conv-1", Text: transcript}}, 0, b, SubjectKindConversation)
	if len(plans) != 1 || len(plans[0].Items) != 1 {
		t.Fatalf("plans = %+v", plans)
	}
	item := plans[0].Items[0]
	if item.Truncated {
		t.Error("a transcript inside the transcript cap was marked truncated")
	}
	if got := len([]rune(item.Text)); got != len([]rune(transcript)) {
		t.Errorf("transcript reached the model as %d runes, want all %d", got, len([]rune(transcript)))
	}
}

func TestPlanBatchesStillCapsCommentsAtTheCommentLimit(t *testing.T) {
	b := loose()
	b.MaxCommentRunes = 600
	b.MaxTranscriptRunes = 20_000
	b.Normalize()

	long := strings.Repeat("a", 2_000)
	plans, _ := PlanBatches([]Item{{ID: "c-1", Text: long}}, 0, b, SubjectKindComment)
	if len(plans) != 1 || len(plans[0].Items) != 1 {
		t.Fatalf("plans = %+v", plans)
	}
	item := plans[0].Items[0]
	if !item.Truncated {
		t.Error("a 2000-rune comment was not marked truncated")
	}
	if got := len([]rune(item.Text)); got != 600 {
		t.Errorf("comment reached the model as %d runes, want 600", got)
	}
}

func TestPlanBatchesStillTruncatesAnEnormousTranscript(t *testing.T) {
	b := loose()
	b.MaxTranscriptRunes = 1_000
	b.Normalize()

	plans, _ := PlanBatches([]Item{{ID: "conv-1", Text: strings.Repeat("z", 5_000)}}, 0, b, SubjectKindConversation)
	item := plans[0].Items[0]
	if !item.Truncated {
		t.Error("a transcript past the transcript cap was not marked truncated")
	}
	if got := len([]rune(item.Text)); got != 1_000 {
		t.Errorf("transcript cut to %d runes, want 1000", got)
	}
}

func TestATranscriptAtTheCapStillFitsInOneCall(t *testing.T) {
	b := DefaultBudget()
	b.Normalize()

	worst := []Item{{ID: "conv-1", Text: strings.Repeat("á", b.MaxTranscriptRunes)}}
	plans, rem := PlanBatches(worst, shared.EstimateTokens(strings.Repeat("x", 4_000)), b, SubjectKindConversation)

	if len(rem.Unplanned) > 0 {
		t.Fatalf("a transcript at the cap could not be planned: %d unplanned, reason %v",
			len(rem.Unplanned), rem.Reason)
	}
	if len(plans) != 1 || len(plans[0].Items) != 1 {
		t.Fatalf("plans = %d, want one call carrying the one conversation", len(plans))
	}
}
