package audience

import (
	"fmt"
	"time"
)

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

type BatchKind string

const (
	BatchKindComment    BatchKind = "comment"
	BatchKindAuthorRole BatchKind = "author_role"
	BatchKindReply      BatchKind = "reply"
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

func NormalizeBatchKind(k BatchKind) BatchKind {
	if !k.Valid() {
		return BatchKindComment
	}
	return k
}

type Batch struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Source      Source    `json:"source"`
	AccountID   string    `json:"accountId"`
	ContainerID string    `json:"containerId"`
	Kind        BatchKind `json:"kind"`

	Model            string       `json:"model"`
	ItemCount        int          `json:"itemCount"`
	PromptTokens     int          `json:"promptTokens"`
	CompletionTokens int          `json:"completionTokens"`
	Outcome          BatchOutcome `json:"outcome"`
	RequestID        string       `json:"requestId,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
}

type BatchTotals struct {
	Batches          int                       `json:"batches"`
	Items            int                       `json:"items"`
	PromptTokens     int                       `json:"promptTokens"`
	CompletionTokens int                       `json:"completionTokens"`
	ByKind           map[BatchKind]BatchTotals `json:"byKind,omitempty"`
}

func (t *BatchTotals) Add(b Batch) {
	t.Batches++
	t.Items += b.ItemCount
	t.PromptTokens += b.PromptTokens
	t.CompletionTokens += b.CompletionTokens

	kind := NormalizeBatchKind(b.Kind)
	if t.ByKind == nil {
		t.ByKind = map[BatchKind]BatchTotals{}
	}
	part := t.ByKind[kind]
	part.Batches++
	part.Items += b.ItemCount
	part.PromptTokens += b.PromptTokens
	part.CompletionTokens += b.CompletionTokens
	t.ByKind[kind] = part
}

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

type Backfill struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Source      Source `json:"source"`
	AccountID   string `json:"accountId"`
	ContainerID string `json:"containerId,omitempty"`

	Status BackfillStatus `json:"status"`
	Cursor string         `json:"cursor,omitempty"`

	EstimatedComments int `json:"estimatedComments"`
	Fetched           int `json:"fetched"`
	Enqueued          int `json:"enqueued"`

	Error string `json:"error,omitempty"`

	RequestedByUserID string     `json:"requestedByUserId"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
}

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

func (b *Backfill) Start(now time.Time) error { return b.transition(BackfillRunning, now) }

func (b *Backfill) Pause(now time.Time) error { return b.transition(BackfillPending, now) }

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
