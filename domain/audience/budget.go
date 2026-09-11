package audience

import (
	"fmt"
	"math"

	"vozko/domain/shared"
)

// The token budget, the core of the brief (§7). Pure functions, no I/O.
//
// Classifying comments one call each is unaffordable and batching them has a
// documented failure mode: past a few dozen items models start returning the
// wrong shape, labels outside the set, or truncated JSON. So batches are
// bounded on FOUR axes (item count, input tokens, output tokens and a
// per-tick cycle ceiling), and a batch closes the moment the next item would
// cross any of them. Items the cycle cannot afford are handed back, not
// dropped: they stay pending and the next tick has a fresh ceiling.

const (
	// HardMaxBatchItems is the ceiling no configuration can raise. Published
	// evaluations of prompt batching put the cliff well under 100.
	HardMaxBatchItems = 40
)

// Budget is the complete set of bounds for one cycle of one container.
type Budget struct {
	// MaxBatchItems is the item count that closes a batch. Default 20.
	MaxBatchItems int
	// MaxInputTokens is the per-call input ceiling, measured AFTER the safety
	// factor and the reserve. Default 12,000.
	MaxInputTokens int
	// MaxOutputTokens is the per-call output ceiling. It bounds
	// MaxTokensForBatch, which is what the provider is told. Default 4,000.
	MaxOutputTokens int
	// PerItemOutputTokens is what one item's answer costs against the schema.
	// Default 64, an ESTIMATE: §16 calibrates it from real completions.
	PerItemOutputTokens int
	// PerItemInputOverhead is what one item's JSON envelope (ref + quoting)
	// costs on the way in, on top of its text. Default 8.
	PerItemInputOverhead int
	// MaxCommentRunes truncates each COMMENT's text before estimation and
	// before the model sees it. Runes, not bytes. Default 600.
	MaxCommentRunes int
	// MaxTranscriptRunes is the same bound for a CONVERSATION, and it is a
	// separate number because the two subjects are nothing alike.
	//
	// A comment is a sentence; 600 runes is generous for one. A transcript is a
	// whole exchange, and 600 runes is its greeting. Sharing the comment cap
	// meant every conversation was classified on its opening lines with the rest
	// cut away, so the model reported no progress, no engagement and no answer,
	// and the attendance score came out zero every time. It was not wrong about
	// what it was shown.
	//
	// Defaults to MaxTranscriptRunes, which is the SAME number the channel
	// adapter renders up to. They must not diverge: whatever gap exists between
	// them is text that was rendered, stored and then silently dropped on the
	// way to the model. Batching already accounts for the real token cost, so a
	// large transcript makes batches smaller rather than making calls overflow.
	MaxTranscriptRunes int
	// ReserveTokens is slack for the schema envelope and closing braces,
	// charged to both the input estimate and the output cap. Default 512.
	ReserveTokens int
	// SafetyFactor scales the input estimate. shared.EstimateTokens already
	// over-counts accented text; this leans further the same way, at the
	// budget layer rather than inside the shared estimator. Default 1.35.
	SafetyFactor float64
	// MaxTokensPerCycle is the ceiling for one job tick (input estimate plus
	// output cap, summed over batches). Default 120,000. Reaching it defers.
	MaxTokensPerCycle int
	// MaxBatchesPerCycle bounds batches per container per tick. Default 12.
	MaxBatchesPerCycle int
}

// DefaultBudget is the plan's numbers.
func DefaultBudget() Budget {
	return Budget{
		MaxBatchItems:        20,
		MaxInputTokens:       12_000,
		MaxOutputTokens:      4_000,
		PerItemOutputTokens:  64,
		PerItemInputOverhead: 8,
		MaxCommentRunes:      600,
		MaxTranscriptRunes:   MaxTranscriptRunes,
		ReserveTokens:        512,
		SafetyFactor:         1.35,
		MaxTokensPerCycle:    120_000,
		MaxBatchesPerCycle:   12,
	}
}

// Normalize fills unset fields from the defaults and clamps the two bounds
// that must never be loosened: the item count above HardMaxBatchItems and a
// safety factor below 1 (which would under-estimate, the one direction the
// budgeter must never lean).
func (b *Budget) Normalize() {
	d := DefaultBudget()
	fill := func(v *int, def int) {
		if *v <= 0 {
			*v = def
		}
	}
	fill(&b.MaxBatchItems, d.MaxBatchItems)
	fill(&b.MaxInputTokens, d.MaxInputTokens)
	fill(&b.MaxOutputTokens, d.MaxOutputTokens)
	fill(&b.PerItemOutputTokens, d.PerItemOutputTokens)
	fill(&b.MaxCommentRunes, d.MaxCommentRunes)
	fill(&b.MaxTranscriptRunes, d.MaxTranscriptRunes)
	fill(&b.MaxTokensPerCycle, d.MaxTokensPerCycle)
	fill(&b.MaxBatchesPerCycle, d.MaxBatchesPerCycle)
	fill(&b.PerItemInputOverhead, d.PerItemInputOverhead)
	fill(&b.ReserveTokens, d.ReserveTokens)
	if b.SafetyFactor < 1 {
		b.SafetyFactor = d.SafetyFactor
	}
	if b.MaxBatchItems > HardMaxBatchItems {
		b.MaxBatchItems = HardMaxBatchItems
	}
}

// Validate checks the budget AS GIVEN (it does not normalise), so a caller
// that skipped Normalize cannot run on a zero.
func (b Budget) Validate() error {
	switch {
	case b.MaxBatchItems <= 0 || b.MaxBatchItems > HardMaxBatchItems:
		return fmt.Errorf("%w: MaxBatchItems must be in 1..%d", ErrBudgetInvalid, HardMaxBatchItems)
	case b.MaxInputTokens <= 0 || b.MaxOutputTokens <= 0:
		return fmt.Errorf("%w: token ceilings must be positive", ErrBudgetInvalid)
	case b.PerItemOutputTokens <= 0:
		return fmt.Errorf("%w: PerItemOutputTokens must be positive", ErrBudgetInvalid)
	case b.PerItemInputOverhead < 0 || b.ReserveTokens < 0:
		return fmt.Errorf("%w: overhead and reserve cannot be negative", ErrBudgetInvalid)
	case b.MaxTranscriptRunes <= 0:
		return fmt.Errorf("%w: MaxTranscriptRunes must be positive", ErrBudgetInvalid)
	case b.MaxCommentRunes <= 0:
		return fmt.Errorf("%w: MaxCommentRunes must be positive", ErrBudgetInvalid)
	case b.SafetyFactor < 1:
		return fmt.Errorf("%w: SafetyFactor must be at least 1", ErrBudgetInvalid)
	case b.MaxBatchesPerCycle <= 0:
		return fmt.Errorf("%w: MaxBatchesPerCycle must be positive", ErrBudgetInvalid)
	case b.ReserveTokens >= b.MaxInputTokens:
		return fmt.Errorf("%w: reserve leaves no input room", ErrBudgetInvalid)
	case b.PerItemOutputTokens+b.ReserveTokens > b.MaxOutputTokens:
		return fmt.Errorf("%w: one item's output does not fit MaxOutputTokens", ErrBudgetInvalid)
	case b.MaxTokensPerCycle < b.MaxInputTokens+b.MaxOutputTokens:
		// Otherwise a full batch could never be planned and the container
		// would starve: a misconfiguration that must fail loudly.
		return fmt.Errorf("%w: MaxTokensPerCycle must fit at least one full batch", ErrBudgetInvalid)
	}
	return nil
}

// MaxTokensForBatch is the hard output cap handed to the provider for a
// batch of n items (§7.1): a runaway generation costs a bounded amount.
func (b Budget) MaxTokensForBatch(n int) int {
	return n*b.PerItemOutputTokens + b.ReserveTokens
}

// WithCycleAllowance narrows MaxTokensPerCycle to what is left of a
// workspace-wide ceiling. The flush job plans several containers in one
// tick against ONE ceiling; each container gets the remainder.
func (b Budget) WithCycleAllowance(remaining int) Budget {
	if remaining < 0 {
		remaining = 0
	}
	if remaining < b.MaxTokensPerCycle {
		b.MaxTokensPerCycle = remaining
	}
	return b
}

// inputEstimate is bound 2's left-hand side: ceil((sys + Σ items) × factor)
// plus the reserve.
func (b Budget) inputEstimate(systemPromptTokens, itemTokens int) int {
	return int(math.Ceil(float64(systemPromptTokens+itemTokens)*b.SafetyFactor)) + b.ReserveTokens
}

// ---- Items and plans ----

// Item is one pending comment as the planner receives it.
type Item struct {
	ID   string
	Text string
}

// PlannedItem is an Item placed in a batch: truncated, estimated and given
// its batch-local ref.
type PlannedItem struct {
	ID        string
	Ref       int // 1..N within the batch; what the model echoes back
	Text      string
	Truncated bool
	Tokens    int // estimated input tokens for the (truncated) text + envelope
}

// BatchPlan is one model call. Its numbers are derived on demand from the
// items and the budget so a Split cannot leave them stale.
type BatchPlan struct {
	Items []PlannedItem

	budget             Budget
	systemPromptTokens int
}

func (p BatchPlan) itemTokens() int {
	n := 0
	for _, it := range p.Items {
		n += it.Tokens
	}
	return n
}

// EstimatedInputTokens is what this call is expected to consume on input,
// after the safety factor and the reserve.
func (p BatchPlan) EstimatedInputTokens() int {
	return p.budget.inputEstimate(p.systemPromptTokens, p.itemTokens())
}

// MaxOutputTokens is the provider cap for this call.
func (p BatchPlan) MaxOutputTokens() int {
	return p.budget.MaxTokensForBatch(len(p.Items))
}

// EstimatedTotalTokens is what the call counts for against the cycle.
func (p BatchPlan) EstimatedTotalTokens() int {
	return p.EstimatedInputTokens() + p.MaxOutputTokens()
}

// IDs returns the row ids in ref order.
func (p BatchPlan) IDs() []string {
	out := make([]string, len(p.Items))
	for i, it := range p.Items {
		out[i] = it.ID
	}
	return out
}

// Split halves the batch for the FinishReason=="length" retry (§7.2). Refs
// are renumbered so each half reads as a fresh 1..N. A one-item batch is
// returned as-is; the caller's attempt counter is what ends that loop.
func (p BatchPlan) Split() []BatchPlan {
	if len(p.Items) <= 1 {
		return []BatchPlan{p}
	}
	mid := (len(p.Items) + 1) / 2
	return []BatchPlan{
		p.Subset(p.Items[:mid]),
		p.Subset(p.Items[mid:]),
	}
}

func (p BatchPlan) Subset(items []PlannedItem) BatchPlan {
	out := BatchPlan{Items: make([]PlannedItem, len(items)), budget: p.budget, systemPromptTokens: p.systemPromptTokens}
	for i, it := range items {
		it.Ref = i + 1
		out.Items[i] = it
	}
	return out
}

// Remainder is what a cycle could not afford, in the order it was received,
// and why. An empty Reason means everything was planned.
type Remainder struct {
	Unplanned []Item
	Reason    string
}

const (
	RemainderCycleTokens  = "cycle_tokens"
	RemainderCycleBatches = "cycle_batches"
)

// TextRunesFor is how much of one subject's text may reach the model.
//
// The one place the comment/transcript distinction is decided, so the estimator
// and the truncation can never disagree about it.
func (b Budget) TextRunesFor(kind SubjectKind) int {
	if kind == SubjectKindConversation {
		return b.MaxTranscriptRunes
	}
	return b.MaxCommentRunes
}

// PlanBatches packs items into batches under every bound in b.
//
// A batch closes when the NEXT item would cross any of:
//  1. len(batch) == MaxBatchItems
//  2. ceil((sys + Σ items + next) × SafetyFactor) + Reserve > MaxInputTokens
//  3. (len(batch)+1) × PerItemOutputTokens + Reserve > MaxOutputTokens
//
// An empty batch always accepts the item, so an oversized comment yields a
// one-item batch rather than an empty plan or an infinite loop.
//
// A closed batch is committed only if the cycle can afford it whole:
// Σ EstimatedTotalTokens ≤ MaxTokensPerCycle and len(plans) < MaxBatchesPerCycle.
// Otherwise it and everything after it are returned in the Remainder.
func PlanBatches(items []Item, systemPromptTokens int, b Budget, kind SubjectKind) ([]BatchPlan, Remainder) {
	if len(items) == 0 {
		return nil, Remainder{}
	}

	var plans []BatchPlan
	cycleTokens := 0
	cur := BatchPlan{budget: b, systemPromptTokens: systemPromptTokens}
	curTokens := 0

	commit := func(firstUnplanned int) (bool, Remainder) {
		if len(plans) >= b.MaxBatchesPerCycle {
			return false, Remainder{Unplanned: cloneItems(items[firstUnplanned:]), Reason: RemainderCycleBatches}
		}
		if cycleTokens+cur.EstimatedTotalTokens() > b.MaxTokensPerCycle {
			return false, Remainder{Unplanned: cloneItems(items[firstUnplanned:]), Reason: RemainderCycleTokens}
		}
		cycleTokens += cur.EstimatedTotalTokens()
		plans = append(plans, cur)
		cur = BatchPlan{budget: b, systemPromptTokens: systemPromptTokens}
		curTokens = 0
		return true, Remainder{}
	}

	batchStart := 0
	for i, raw := range items {
		text, cut := TruncateRunes(raw.Text, b.TextRunesFor(kind))
		tokens := shared.EstimateTokens(text) + b.PerItemInputOverhead

		if len(cur.Items) > 0 && (len(cur.Items) >= b.MaxBatchItems ||
			b.inputEstimate(systemPromptTokens, curTokens+tokens) > b.MaxInputTokens ||
			b.MaxTokensForBatch(len(cur.Items)+1) > b.MaxOutputTokens) {
			if ok, rem := commit(batchStart); !ok {
				return plans, rem
			}
			batchStart = i
		}

		cur.Items = append(cur.Items, PlannedItem{
			ID:        raw.ID,
			Ref:       len(cur.Items) + 1,
			Text:      text,
			Truncated: cut,
			Tokens:    tokens,
		})
		curTokens += tokens
	}

	if ok, rem := commit(batchStart); !ok {
		return plans, rem
	}
	return plans, Remainder{}
}

func cloneItems(items []Item) []Item {
	out := make([]Item, len(items))
	copy(out, items)
	return out
}
