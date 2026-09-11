package audience

import (
	"testing"
	"time"
)

func TestBatchOutcome_Valid(t *testing.T) {
	for _, o := range []BatchOutcome{OutcomeOK, OutcomeLength, OutcomeParseError, OutcomeProviderError} {
		if !o.Valid() {
			t.Errorf("%q should be valid", o)
		}
	}
	if BatchOutcome("timeout").Valid() || BatchOutcome("").Valid() {
		t.Error("unknown outcome should be invalid")
	}
}

// The receipt is what the customer sees on the bill: "12.480 comentários
// analisados · R$ 37,44 este mês". Charging for something invisible is how
// disputes start (§9.4).
func TestBatchTotals_Add(t *testing.T) {
	var tot BatchTotals
	tot.Add(Batch{ItemCount: 20, PromptTokens: 1000, CompletionTokens: 400, PriceMicros: 100})
	tot.Add(Batch{ItemCount: 5, PromptTokens: 300, CompletionTokens: 90, PriceMicros: 25})
	if tot.Batches != 2 || tot.Items != 25 || tot.PromptTokens != 1300 ||
		tot.CompletionTokens != 490 || tot.PriceMicros != 125 {
		t.Fatalf("totals = %+v", tot)
	}
}

// The author pass (§5) buys tokens from the same budget as the comment pass,
// and a customer asking what they are paying for is owed the split. The
// per-kind parts must always add back up to the whole.
func TestBatchTotals_SplitsByKind(t *testing.T) {
	var tot BatchTotals
	// An untagged row is the comment pass: that is what every row written
	// before the author pass existed is.
	tot.Add(Batch{ItemCount: 20, PromptTokens: 1000, CompletionTokens: 400, PriceMicros: 100})
	tot.Add(Batch{Kind: BatchKindComment, ItemCount: 5, PromptTokens: 300, CompletionTokens: 90, PriceMicros: 25})
	tot.Add(Batch{Kind: BatchKindAuthorRole, ItemCount: 40, PromptTokens: 2000, CompletionTokens: 60, PriceMicros: 200})

	comments := tot.ByKind[BatchKindComment]
	if comments.Batches != 2 || comments.Items != 25 || comments.PriceMicros != 125 {
		t.Fatalf("comment pass = %+v", comments)
	}
	roles := tot.ByKind[BatchKindAuthorRole]
	if roles.Batches != 1 || roles.Items != 40 || roles.PriceMicros != 200 {
		t.Fatalf("author pass = %+v", roles)
	}
	if comments.PriceMicros+roles.PriceMicros != tot.PriceMicros {
		t.Fatalf("the parts (%d + %d) must add up to the whole (%d)",
			comments.PriceMicros, roles.PriceMicros, tot.PriceMicros)
	}
	if comments.Batches+roles.Batches != tot.Batches {
		t.Fatal("batch counts must add up too")
	}
}

// ---- Backfill ----

// Backfill is operator-initiated, estimated first, resumable and cancellable
// (§10). Its status machine is the small part of that which is pure.
func TestBackfillStatus_CanTransitionTo(t *testing.T) {
	allowed := map[BackfillStatus][]BackfillStatus{
		BackfillPending:  {BackfillRunning, BackfillCanceled},
		BackfillRunning:  {BackfillPending, BackfillDone, BackfillFailed, BackfillCanceled},
		BackfillDone:     {},
		BackfillFailed:   {BackfillPending},
		BackfillCanceled: {},
	}
	all := []BackfillStatus{BackfillPending, BackfillRunning, BackfillDone, BackfillFailed, BackfillCanceled}
	for from, tos := range allowed {
		ok := map[BackfillStatus]bool{}
		for _, to := range tos {
			ok[to] = true
		}
		for _, to := range all {
			if got := from.CanTransitionTo(to); got != ok[to] {
				t.Errorf("%s -> %s = %v, want %v", from, to, got, ok[to])
			}
		}
	}
}

func TestBackfill_Progress(t *testing.T) {
	b := Backfill{Status: BackfillRunning, EstimatedComments: 200, Fetched: 50}
	if got := b.Progress(); got != 0.25 {
		t.Fatalf("Progress() = %v, want 0.25", got)
	}
	b.EstimatedComments = 0
	if got := b.Progress(); got != 0 {
		t.Fatalf("unknown estimate must not divide by zero: %v", got)
	}
	b.Fetched, b.EstimatedComments = 300, 200
	if got := b.Progress(); got != 1 {
		t.Fatalf("progress must clamp at 1, got %v", got)
	}
}

// A backfill that stops mid-page keeps its cursor, so a restart continues
// rather than starting over (and re-paying for) the whole edge.
func TestBackfill_Advance(t *testing.T) {
	b := Backfill{Status: BackfillRunning}
	b.Advance("cursor-2", 25, 20, now)
	if b.Cursor != "cursor-2" || b.Fetched != 25 || b.Enqueued != 20 || !b.UpdatedAt.Equal(now) {
		t.Fatalf("after advance: %+v", b)
	}
	b.Advance("", 10, 10, now.Add(time.Minute))
	if b.Fetched != 35 || b.Enqueued != 30 {
		t.Fatalf("counts must accumulate: %+v", b)
	}
}

func TestBackfill_Finish(t *testing.T) {
	b := Backfill{Status: BackfillRunning}
	if err := b.Finish(BackfillDone, "", now); err != nil {
		t.Fatal(err)
	}
	if b.Status != BackfillDone || b.FinishedAt == nil {
		t.Fatalf("after finish: %+v", b)
	}
	if err := b.Finish(BackfillFailed, "boom", now); err == nil {
		t.Fatal("a finished backfill cannot finish again")
	}
}
