package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/messaging"
	uw "vozko/domain/unofficial_whatsapp"
)

const scriptedBatchTimeout = 10 * time.Minute

type SeedInboxPublisher struct {
	pub messaging.MessageQueuePub
}

func NewSeedInboxPublisher(pub messaging.MessageQueuePub) *SeedInboxPublisher {
	return &SeedInboxPublisher{pub: pub}
}

func (p *SeedInboxPublisher) Publish(in uw.SeedRequest) (uw.SeedQueued, error) {
	var queued uw.SeedQueued
	if p == nil || p.pub == nil {
		return queued, nil
	}

	in.Normalize()
	if err := in.Validate(); err != nil {
		if errors.Is(err, uw.ErrSeedNoTargets) {
			return queued, nil
		}
		return queued, err
	}

	for _, batch := range in.Split() {
		payload, err := json.Marshal(batch)
		if err != nil {
			return queued, fmt.Errorf("unofficial whatsapp: could not encode a seed batch: %w", err)
		}
		if err := p.pub.Publish(uw.SeedTopic, payload); err != nil {
			return queued, err
		}
		queued.Targets += len(batch.Targets)
		if batch.Script != nil {
			queued.Scripted += len(batch.Targets)
		}
	}
	return queued, nil
}

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

func (c *ConsumeSeedInboxUseCase) handle(payload []byte, ack messaging.MessageAck) {
	var req uw.SeedRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		log.Printf("[unofficial-whatsapp] dropping unreadable inbox seed batch: %v", err)
		_ = ack.Nack(false)
		return
	}

	ctx := context.Background()
	if req.Script != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, scriptedBatchTimeout)
		defer cancel()
	}

	out, err := c.seed.Execute(ctx, req)
	if err != nil {
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

	log.Printf("[unofficial-whatsapp] inbox seed batch for workspace %s: %d seeded, %d already active, %d failed, %d scripted, %d script failed, %d skipped for having no name",
		req.WorkspaceID, out.Seeded, out.AlreadyActive, out.Failed,
		out.Scripted, out.ScriptFailed, out.ScriptSkippedNoName)
	_ = ack.Ack()
}
