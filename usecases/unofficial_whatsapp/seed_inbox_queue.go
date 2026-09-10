package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"vozko/domain/messaging"
	uw "vozko/domain/unofficial_whatsapp"
)

// Seeding runs off the queue rather than inside the import request, because an
// import accepts a hundred thousand rows and each one is a contact lookup, a
// lead bridge, a conversation and a message. Holding an HTTP connection open
// for that is how an operator learns their import "failed" at the sixty second
// gateway timeout while it was in fact still running.

// SeedInboxPublisher hands one import's worth of numbers to the queue.
type SeedInboxPublisher struct {
	pub messaging.MessageQueuePub
}

func NewSeedInboxPublisher(pub messaging.MessageQueuePub) *SeedInboxPublisher {
	return &SeedInboxPublisher{pub: pub}
}

// Publish normalises, splits and enqueues, and reports how many targets it
// accepted.
//
// The count is what the import response tells the operator, so it is the count
// of targets that actually reached the queue, never the count that was asked
// for. A request that normalises down to nothing returns 0 with no error: every
// row it dropped was already reported by the import's own rejection list, so
// failing here would report the same bad rows twice under a different name.
func (p *SeedInboxPublisher) Publish(in uw.SeedRequest) (int, error) {
	if p == nil || p.pub == nil {
		return 0, nil
	}

	in.Normalize()
	if err := in.Validate(); err != nil {
		if errors.Is(err, uw.ErrSeedNoTargets) {
			return 0, nil
		}
		return 0, err
	}

	queued := 0
	for _, batch := range in.Split() {
		payload, err := json.Marshal(batch)
		if err != nil {
			return queued, fmt.Errorf("unofficial whatsapp: could not encode a seed batch: %w", err)
		}
		if err := p.pub.Publish(uw.SeedTopic, payload); err != nil {
			// Partial progress is kept and reported. The batches already
			// published will seed; saying "0" here would understate what is
			// about to appear in the operator's inbox.
			return queued, err
		}
		queued += len(batch.Targets)
	}
	return queued, nil
}

// ConsumeSeedInboxUseCase drains the seeding topic.
type ConsumeSeedInboxUseCase struct {
	sub  messaging.MessageQueueSub
	seed *SeedInboxUseCase
}

func NewConsumeSeedInboxUseCase(
	sub messaging.MessageQueueSub,
	seed *SeedInboxUseCase,
) *ConsumeSeedInboxUseCase {
	return &ConsumeSeedInboxUseCase{sub: sub, seed: seed}
}

func (c *ConsumeSeedInboxUseCase) Start() error {
	if c == nil || c.sub == nil || c.seed == nil {
		return nil
	}
	go func() {
		if err := c.sub.Subscribe(uw.SeedTopic, c.handle); err != nil {
			log.Printf("[unofficial-whatsapp] inbox seed subscribe failed: %v", err)
		}
	}()
	return nil
}

// handle seeds one batch.
//
// Processed inline rather than fanned onto goroutines: two batches for the same
// workspace running at once would race on FindOrCreate for numbers that appear
// in both, and the whole point of seeding is that it is not urgent. One batch at
// a time also keeps a hundred thousand row import from opening five hundred
// concurrent transactions against the database the inbox is being read from.
func (c *ConsumeSeedInboxUseCase) handle(payload []byte, ack messaging.MessageAck) {
	var req uw.SeedRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		// A malformed payload will never parse on a retry.
		log.Printf("[unofficial-whatsapp] dropping unreadable inbox seed batch: %v", err)
		_ = ack.Nack(false)
		return
	}

	out, err := c.seed.Execute(context.Background(), req)
	if err != nil {
		// Redelivery cannot fix a workspace with no connected number, an empty
		// batch or a missing workspace id, so those are dropped rather than
		// spun on. Anything else is a real failure and worth one more attempt.
		if errors.Is(err, ErrNoConnectedInstance) ||
			errors.Is(err, uw.ErrSeedNoTargets) ||
			errors.Is(err, uw.ErrWorkspaceIDRequired) {
			log.Printf("[unofficial-whatsapp] dropping inbox seed batch for workspace %s: %v",
				req.WorkspaceID, err)
			_ = ack.Nack(false)
			return
		}
		log.Printf("[unofficial-whatsapp] inbox seed batch failed for workspace %s, requeueing: %v",
			req.WorkspaceID, err)
		_ = ack.Nack(true)
		return
	}

	// Requeueing on a partial failure would re-seed everything that succeeded.
	// That is safe (seedOne skips a conversation that already has a placeholder)
	// but pointless, and the failures are logged per target where they happened.
	log.Printf("[unofficial-whatsapp] inbox seed batch for workspace %s: %d seeded, %d already active, %d failed",
		req.WorkspaceID, out.Seeded, out.AlreadyActive, out.Failed)
	_ = ack.Ack()
}
