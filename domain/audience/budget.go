package audience

import (
	"fmt"
	"math"

	"vozko/domain/shared"
)

const (
	HardMaxBatchItems = 40
)

type Budget struct {
	MaxBatchItems        int
	MaxInputTokens       int
	MaxOutputTokens      int
	PerItemOutputTokens  int
	PerItemInputOverhead int
	MaxCommentRunes      int
	MaxTranscriptRunes   int
	ReserveTokens        int
	SafetyFactor         float64
	MaxTokensPerCycle    int
	MaxBatchesPerCycle   int
}

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
		return fmt.Errorf("%w: MaxTokensPerCycle must fit at least one full batch", ErrBudgetInvalid)
	}
	return nil
}

func (b Budget) MaxTokensForBatch(n int) int {
	return n*b.PerItemOutputTokens + b.ReserveTokens
}

func (b Budget) WithCycleAllowance(remaining int) Budget {
	if remaining < 0 {
		remaining = 0
	}
	if remaining < b.MaxTokensPerCycle {
		b.MaxTokensPerCycle = remaining
	}
	return b
}

func (b Budget) inputEstimate(systemPromptTokens, itemTokens int) int {
	return int(math.Ceil(float64(systemPromptTokens+itemTokens)*b.SafetyFactor)) + b.ReserveTokens
}

type Item struct {
	ID   string
	Text string
}

type PlannedItem struct {
	ID        string
	Ref       int
	Text      string
	Truncated bool
	Tokens    int
}

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

func (p BatchPlan) EstimatedInputTokens() int {
	return p.budget.inputEstimate(p.systemPromptTokens, p.itemTokens())
}

func (p BatchPlan) MaxOutputTokens() int {
	return p.budget.MaxTokensForBatch(len(p.Items))
}

func (p BatchPlan) EstimatedTotalTokens() int {
	return p.EstimatedInputTokens() + p.MaxOutputTokens()
}

func (p BatchPlan) IDs() []string {
	out := make([]string, len(p.Items))
	for i, it := range p.Items {
		out[i] = it.ID
	}
	return out
}

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

type Remainder struct {
	Unplanned []Item
	Reason    string
}

const (
	RemainderCycleTokens  = "cycle_tokens"
	RemainderCycleBatches = "cycle_batches"
)

func (b Budget) TextRunesFor(kind SubjectKind) int {
	if kind == SubjectKindConversation {
		return b.MaxTranscriptRunes
	}
	return b.MaxCommentRunes
}

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
