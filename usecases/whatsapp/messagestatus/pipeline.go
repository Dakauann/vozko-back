package messagestatus_usecase

import (
	"context"
	"fmt"
	"time"

	"vozko/domain/whatsapp/messagestatus"
)

type Pipeline struct {
	ledger messagestatus.Ledger
	now    func() time.Time
}

func NewPipeline(ledger messagestatus.Ledger) *Pipeline {
	return &Pipeline{ledger: ledger, now: time.Now}
}

func (p *Pipeline) Sweep(ctx context.Context) error {
	now := p.now().UTC()
	if _, err := p.ledger.AttributePending(ctx, now.Add(-messagestatus.AttributionWait)); err != nil {
		return fmt.Errorf("attribute template message statuses: %w", err)
	}
	if _, err := p.ledger.RollUpPending(ctx, now.Add(-messagestatus.RecountHorizon)); err != nil {
		return fmt.Errorf("roll up template message statuses: %w", err)
	}
	return nil
}

func (p *Pipeline) Purge(ctx context.Context) error {
	if _, err := p.ledger.PurgeRolledUpBefore(ctx, p.now().UTC().Add(-messagestatus.RawRetention)); err != nil {
		return fmt.Errorf("purge template message statuses: %w", err)
	}
	return nil
}
