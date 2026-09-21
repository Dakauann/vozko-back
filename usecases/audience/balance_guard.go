package audience_usecase

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	ca "vozko/domain/audience"
	"vozko/domain/balance"
	balance_usecase "vozko/usecases/balance"
)

// The balance floor, shared by every path in the engine that spends tokens.
//
// It exists as its own type because it was NOT shared, and that was a hole. The
// comment pass has always failed closed on an empty balance (Engine.guards),
// but the three AI paths added after it did not: a workspace at zero could
// still be drafting replies, inferring author roles and writing alert briefings.
// Every one of those calls carries the workspace id, so it WAS billed; nothing
// was free. What was missing is the refusal, which is what stops a workspace
// running further into the red one call at a time.
//
// Fail-closed on purpose: a balance we cannot read is treated as too low, the
// same choice Engine.guards makes, because the alternative is spending money we
// cannot prove the customer has.

// AlertFreshness aside, this is the one number the whole engine agrees on.
//
// A thin wrapper over balance_usecase.SpendGuard rather than a second
// implementation of it: the decision (is this workspace above the floor, and is
// an unreadable balance a refusal) is product-wide, while the ca.ErrBalanceBelowFloor
// return is this engine's own vocabulary. The wrapper is the translation, and
// nothing else.
type balanceGuard struct {
	inner balance_usecase.SpendGuard
}

func newBalanceGuard(checker balance.CachedBalanceChecker, label string) balanceGuard {
	return balanceGuard{inner: balance_usecase.NewSpendGuard(checker, label)}
}

// Allow reports whether this workspace may spend on a model call right now.
//
// A nil checker allows: a deployment without balance tracking is not a
// deployment where every AI feature should be dead.
func (g balanceGuard) Allow(workspaceID string) error {
	if g.inner.Allow(workspaceID) {
		return nil
	}
	return ca.ErrBalanceBelowFloor
}

// aiCall is one model call to book, for the paths that are not the batched
// comment pass: a drafted reply, an alert briefing, an author-role pass.
type aiCall struct {
	WorkspaceID      string
	Source           ca.Source
	AccountID        string
	ContainerID      string
	Kind             ca.BatchKind
	Model            string
	ItemCount        int
	PromptTokens     int
	CompletionTokens int
}

// newAIBatch builds the receipt. The ONE place a single-call receipt is
// assembled, so its id, timestamp and outcome cannot be set three ways.
func newAIBatch(clock ca.Clock, call aiCall) *ca.Batch {
	now := time.Now().UTC()
	if clock != nil {
		now = clock.Now()
	}
	items := call.ItemCount
	if items <= 0 {
		items = 1
	}
	return &ca.Batch{
		ID:               uuid.New().String(),
		WorkspaceID:      call.WorkspaceID,
		Source:           call.Source,
		AccountID:        call.AccountID,
		ContainerID:      call.ContainerID,
		Kind:             call.Kind,
		Model:            call.Model,
		ItemCount:        items,
		PromptTokens:     call.PromptTokens,
		CompletionTokens: call.CompletionTokens,
		Outcome:          ca.OutcomeOK,
		CreatedAt:        now,
	}
}

// writeBatch persists a receipt. The ONE place a receipt is written, used by
// the comment pass and by every single-call path.
//
// Best effort and deliberately silent on failure: the tokens were already spent
// and billed by the AI adapter, so losing the receipt costs a line on the spend
// page rather than the money itself. Failing the caller here would throw away
// work that has already been paid for.
func writeBatch(ctx context.Context, batches ca.BatchRepository, batch *ca.Batch) {
	if batches == nil || batch == nil || batch.WorkspaceID == "" {
		return
	}
	if err := batches.Create(ctx, batch); err != nil {
		log.Printf("[comment-analysis] %s receipt %s not recorded: %v", batch.Kind, batch.ID, err)
	}
}

// recordAICall is the common case: build it and write it.
//
// Every pass is billed the same way, by TOKENS, on the workspace ledger the
// moment the AI adapter sees the workspace id. The receipt carries the token
// counts and nothing else: there is no money on it to reconcile.
func recordAICall(ctx context.Context, batches ca.BatchRepository, clock ca.Clock, call aiCall) {
	writeBatch(ctx, batches, newAIBatch(clock, call))
}
