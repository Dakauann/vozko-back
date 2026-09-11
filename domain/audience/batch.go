package audience

import (
	"fmt"
	"time"
)

// ---- Batch: the receipt for one model call (§9.4) ----

// BatchOutcome is what happened to a call. Mirrors the
// audience_batches_total{outcome} metric.
type BatchOutcome string

const (
	OutcomeOK            BatchOutcome = "ok"
	OutcomeLength        BatchOutcome = "length"
	OutcomeParseError    BatchOutcome = "parse_error"
	OutcomeProviderError BatchOutcome = "provider_error"
)

func (o BatchOutcome) Valid() bool {
	switch o {
	case OutcomeOK, OutcomeLength, OutcomeParseError, OutcomeProviderError:
		return true
	}
	return false
}

// Batch is one model call as billed: what was sent, what came back, what it
// cost. It is what lets the dashboard say "12.480 comentários analisados ·
// R$ 37,44 este mês"; charging for something invisible is how disputes start.
// BatchKind says which pass bought these tokens. The comment pass and the
// author pass (§5) are separate consumers of the same budget, and a customer
// asking "what am I paying for" is owed the split rather than one number.
type BatchKind string

const (
	BatchKindComment    BatchKind = "comment"
	BatchKindAuthorRole BatchKind = "author_role"
	// BatchKindReply is a drafted public answer (§6).
	BatchKindReply BatchKind = "reply"
	// BatchKindAlertBrief is the model's reading attached to an alert.
	BatchKindAlertBrief BatchKind = "alert_brief"
)

func AllBatchKinds() []BatchKind {
	return []BatchKind{BatchKindComment, BatchKindAuthorRole, BatchKindReply, BatchKindAlertBrief}
}

func (k BatchKind) Valid() bool {
	switch k {
	case BatchKindComment, BatchKindAuthorRole, BatchKindReply, BatchKindAlertBrief:
		return true
	}
	return false
}

// NormalizeBatchKind defaults to the comment pass, which is what every row
// written before the author pass existed is.
func NormalizeBatchKind(k BatchKind) BatchKind {
	if !k.Valid() {
		return BatchKindComment
	}
	return k
}

type Batch struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`
	ContainerID string `json:"containerId"`
	// Kind is the pass. Empty reads as the comment pass.
	Kind BatchKind `json:"kind"`

	Model            string       `json:"model"`
	ItemCount        int          `json:"itemCount"`
	PromptTokens     int          `json:"promptTokens"`
	CompletionTokens int          `json:"completionTokens"`
	PriceMicros      int64        `json:"priceMicros"`
	Outcome          BatchOutcome `json:"outcome"`
	RequestID        string       `json:"requestId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

// BatchTotals is a period's spend.
type BatchTotals struct {
	Batches          int   `json:"batches"`
	Items            int   `json:"items"`
	PromptTokens     int   `json:"promptTokens"`
	CompletionTokens int   `json:"completionTokens"`
	PriceMicros      int64 `json:"priceMicros"`
	// ByKind splits the same period by pass, so the dashboard can show what
	// the author inference cost separately from the comment classification.
	// Always present for every kind, zeroed when a pass did not run.
	ByKind map[BatchKind]BatchTotals `json:"byKind,omitempty"`
}

func (t *BatchTotals) Add(b Batch) {
	t.Batches++
	t.Items += b.ItemCount
	t.PromptTokens += b.PromptTokens
	t.CompletionTokens += b.CompletionTokens
	t.PriceMicros += b.PriceMicros

	kind := NormalizeBatchKind(b.Kind)
	if t.ByKind == nil {
		t.ByKind = map[BatchKind]BatchTotals{}
	}
	part := t.ByKind[kind]
	part.Batches++
	part.Items += b.ItemCount
	part.PromptTokens += b.PromptTokens
	part.CompletionTokens += b.CompletionTokens
	part.PriceMicros += b.PriceMicros
	t.ByKind[kind] = part
}

// ---- Backfill (§10) ----

// BackfillStatus is where a backfill is.
//
//	pending ──▶ running ──▶ done
//	   │           ├──────▶ failed ──▶ pending   (operator retry)
//	   │           ├──────▶ pending             (rate-limit pause, resumed next tick)
//	   └───────────┴──────▶ canceled
type BackfillStatus string

const (
	BackfillPending  BackfillStatus = "pending"
	BackfillRunning  BackfillStatus = "running"
	BackfillDone     BackfillStatus = "done"
	BackfillFailed   BackfillStatus = "failed"
	BackfillCanceled BackfillStatus = "canceled"
)

func (s BackfillStatus) Valid() bool {
	switch s {
	case BackfillPending, BackfillRunning, BackfillDone, BackfillFailed, BackfillCanceled:
		return true
	}
	return false
}

func (s BackfillStatus) IsTerminal() bool {
	return s == BackfillDone || s == BackfillCanceled
}

func (s BackfillStatus) CanTransitionTo(next BackfillStatus) bool {
	switch s {
	case BackfillPending:
		return next == BackfillRunning || next == BackfillCanceled
	case BackfillRunning:
		return next == BackfillPending || next == BackfillDone || next == BackfillFailed || next == BackfillCanceled
	case BackfillFailed:
		return next == BackfillPending
	default:
		return false
	}
}

// Backfill is one operator-initiated pass over a container's (or an
// account's) historical comments. It ENQUEUES; it never classifies, so there
// is one classifier, one budgeter and one billing path.
type Backfill struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`
	// ContainerID empty means the whole account.
	ContainerID string `json:"containerId,omitempty"`

	Status BackfillStatus `json:"status"`
	// Cursor is the channel's pagination token. A restart continues from it.
	Cursor string `json:"cursor,omitempty"`

	// EstimatedComments is what the operator confirmed: the sum of the local
	// projection's comment counts, shown with a price before anything runs.
	EstimatedComments int `json:"estimatedComments"`
	Fetched           int `json:"fetched"`
	Enqueued          int `json:"enqueued"`

	Error string `json:"error,omitempty"`

	RequestedByUserID string     `json:"requestedByUserId"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
}

// Progress is fetched over estimated, in [0,1]; 0 when the estimate is
// unknown.
func (b Backfill) Progress() float64 {
	if b.EstimatedComments <= 0 {
		return 0
	}
	p := float64(b.Fetched) / float64(b.EstimatedComments)
	if p > 1 {
		return 1
	}
	return p
}

// Advance records one fetched page.
func (b *Backfill) Advance(cursor string, fetched, enqueued int, now time.Time) {
	b.Cursor = cursor
	b.Fetched += fetched
	b.Enqueued += enqueued
	b.UpdatedAt = now
}

func (b *Backfill) transition(next BackfillStatus, now time.Time) error {
	if !b.Status.CanTransitionTo(next) {
		return fmt.Errorf("%w: backfill %s -> %s", ErrStatusTransition, b.Status, next)
	}
	b.Status = next
	b.UpdatedAt = now
	return nil
}

// Start claims the backfill for a run.
func (b *Backfill) Start(now time.Time) error { return b.transition(BackfillRunning, now) }

// Pause hands the backfill back to pending (rate-limit budget exhausted for
// this hour); the cursor is kept.
func (b *Backfill) Pause(now time.Time) error { return b.transition(BackfillPending, now) }

// Finish ends the run in a terminal-or-failed state.
func (b *Backfill) Finish(status BackfillStatus, errMsg string, now time.Time) error {
	if status != BackfillDone && status != BackfillFailed && status != BackfillCanceled {
		return fmt.Errorf("%w: %q is not a finishing status", ErrStatusTransition, status)
	}
	if err := b.transition(status, now); err != nil {
		return err
	}
	b.Error = errMsg
	b.FinishedAt = &now
	return nil
}
