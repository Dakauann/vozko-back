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

type balanceGuard struct {
	inner balance_usecase.SpendGuard
}

func newBalanceGuard(checker balance.CachedBalanceChecker, label string) balanceGuard {
	return balanceGuard{inner: balance_usecase.NewSpendGuard(checker, label)}
}

func (g balanceGuard) Allow(workspaceID string) error {
	if g.inner.Allow(workspaceID) {
		return nil
	}
	return ca.ErrBalanceBelowFloor
}

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

func writeBatch(ctx context.Context, batches ca.BatchRepository, batch *ca.Batch) {
	if batches == nil || batch == nil || batch.WorkspaceID == "" {
		return
	}
	if err := batches.Create(ctx, batch); err != nil {
		log.Printf("[comment-analysis] %s receipt %s not recorded: %v", batch.Kind, batch.ID, err)
	}
}

func recordAICall(ctx context.Context, batches ca.BatchRepository, clock ca.Clock, call aiCall) {
	writeBatch(ctx, batches, newAIBatch(clock, call))
}
