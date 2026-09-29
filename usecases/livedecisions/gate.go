package livedecisions_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"vozko/domain/cache"
	ld "vozko/domain/livedecision"
)

const (
	pendingKey     = "live:decide:pending"
	firstKeyPrefix = "live:decide:first:"
	lockKeyPrefix  = "lock:live_decide:"
)

type Stamper interface {
	Stamp(ref ld.EntryRef) error
}

type Gate struct {
	service *Service
	stamper Stamper
}

func NewGate(service *Service, stamper Stamper) *Gate {
	return &Gate{service: service, stamper: stamper}
}

func (g *Gate) Queue(ref ld.EntryRef) {
	if g == nil || g.service == nil || !g.service.wired() || ref.EntryID == "" || ref.EntryType == "" {
		return
	}
	if err := g.stamper.Stamp(ref); err != nil {
		log.Printf("[live-decisions] queueing entry %s failed: %v", ref.EntryID, err)
	}
}

type CoalescerConfig struct {
	Quiet   time.Duration
	MaxWait time.Duration
	Workers int
	Clock   func() time.Time
}

type Coalescer struct {
	state  cache.SharedState
	ack    cache.HashFieldAcknowledger
	decide func(ctx context.Context, ref ld.EntryRef)
	cfg    CoalescerConfig
}

func NewCoalescer(state cache.SharedState, decide func(ctx context.Context, ref ld.EntryRef), cfg CoalescerConfig) (*Coalescer, error) {
	ack, ok := state.(cache.HashFieldAcknowledger)
	if !ok {
		return nil, errors.New("live decisions: the shared state cannot acknowledge hash fields atomically")
	}
	if cfg.Quiet <= 0 {
		cfg.Quiet = 4 * time.Second
	}
	if cfg.MaxWait <= 0 {
		cfg.MaxWait = 15 * time.Second
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 8
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &Coalescer{state: state, ack: ack, decide: decide, cfg: cfg}, nil
}

type pendingDecision struct {
	Ref ld.EntryRef `json:"ref"`
	Due time.Time   `json:"due"`
}

func field(ref ld.EntryRef) string {
	return ref.EntryType + "|" + ref.EntryID
}

func (c *Coalescer) Stamp(ref ld.EntryRef) error {
	now := c.cfg.Clock()
	key := firstKeyPrefix + field(ref)
	if _, err := c.state.SetNX(key, now.Format(time.RFC3339Nano), c.cfg.MaxWait+time.Minute); err != nil {
		return err
	}
	first := now
	if raw, err := c.state.GetString(key); err == nil {
		if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			first = parsed
		}
	}
	due := now.Add(c.cfg.Quiet)
	if limit := first.Add(c.cfg.MaxWait); limit.Before(due) {
		due = limit
	}
	value, err := json.Marshal(pendingDecision{Ref: ref, Due: due})
	if err != nil {
		return err
	}
	return c.state.HSet(pendingKey, field(ref), string(value))
}

func (c *Coalescer) Flush(ctx context.Context) int {
	all, err := c.state.HGetAll(pendingKey)
	if err != nil {
		log.Printf("[live-decisions] reading the queue failed: %v", err)
		return 0
	}
	now := c.cfg.Clock()
	slots := make(chan struct{}, c.cfg.Workers)
	var wg sync.WaitGroup
	dispatched := 0
	for name, raw := range all {
		var pending pendingDecision
		if err := json.Unmarshal([]byte(raw), &pending); err != nil {
			_ = c.ack.HDelIfValue(pendingKey, name, raw)
			continue
		}
		if pending.Due.After(now) {
			continue
		}
		claimed, err := c.state.SetNX(lockKeyPrefix+name, "1", 30*time.Second)
		if err != nil || !claimed {
			continue
		}
		if !c.stillPending(name, raw) {
			_ = c.state.Del(lockKeyPrefix + name)
			continue
		}
		if err := c.ack.HDelIfValue(pendingKey, name, raw); err != nil {
			_ = c.state.Del(lockKeyPrefix + name)
			continue
		}
		_ = c.state.Del(firstKeyPrefix + name)
		dispatched++
		wg.Add(1)
		slots <- struct{}{}
		go func(name string, ref ld.EntryRef) {
			defer wg.Done()
			defer func() { <-slots }()
			defer func() { _ = c.state.Del(lockKeyPrefix + name) }()
			c.decide(ctx, ref)
		}(name, pending.Ref)
	}
	wg.Wait()
	return dispatched
}

func (c *Coalescer) stillPending(name, raw string) bool {
	current, err := c.state.HGetAll(pendingKey)
	return err == nil && current[name] == raw
}

func (c *Coalescer) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Flush(ctx)
		}
	}
}

type GateHandle struct {
	gate atomic.Pointer[Gate]
}

func (h *GateHandle) Bind(gate *Gate) {
	h.gate.Store(gate)
}

func (h *GateHandle) current() *Gate {
	if h == nil {
		return nil
	}
	return h.gate.Load()
}

func (h *GateHandle) Queue(ref ld.EntryRef) {
	h.current().Queue(ref)
}

func (h *GateHandle) Acts(ctx context.Context, workspaceID string) bool {
	gate := h.current()
	return gate != nil && gate.service.Acts(ctx, workspaceID)
}

func (h *GateHandle) StageNeedsReview(ctx context.Context, target Trigger) bool {
	gate := h.current()
	return gate == nil || gate.service.StageNeedsReview(ctx, target)
}

func (h *GateHandle) QuietGate(ctx context.Context, target Trigger, wantMemory, wantDeals bool, memory, deals string) ld.QuietGate {
	gate := h.current()
	if gate == nil {
		return ld.QuietGate{NeedsMemory: wantMemory, NeedsDeals: wantDeals}
	}
	return gate.service.QuietGate(ctx, target, wantMemory, wantDeals, memory, deals)
}
