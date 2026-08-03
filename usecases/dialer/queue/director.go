// Package queue is the POLICY brain of the call queue (ACD). It owns the ordered
// waiting line (caller position), the target -> available-candidate resolution, the
// bounds that guarantee no caller waits forever, and the lifecycle event log. It
// carries NO media/SIP knowledge: the transfer engine owns the parked leg and the
// ring waves and consults this Director through the dialer.QueueDirector port, so
// the two layers stay cleanly separated and independently testable.
//
// A queued caller is a parked transfer leg (identical to a recall) whose ring
// target is a pool of agents instead of the original initiator. See
// docs/CALL_TRANSFER_PRODUCTION_PLAN.md.
package queue

import (
	"context"
	"log"
	"sync"
	"time"

	dialer "vozko/domain/dialer"
)

// PolicyResolver resolves the queue policy for a target (from config / DB). A
// policy with Enabled=false means "do not queue". Behind a port so the director
// carries no config-store knowledge.
type PolicyResolver interface {
	Resolve(ctx context.Context, workspaceID string, target dialer.QueueTarget) dialer.QueuePolicy
}

// CandidateSource returns the agent user ids currently AVAILABLE to take a queued
// caller for a target (online, idle, not reserved), already scoped to the target (a
// department's members, one agent, or the whole workspace). This is the same
// availability the roulette uses; behind a port so the director carries no
// session/registry knowledge.
type CandidateSource interface {
	AvailableForTarget(ctx context.Context, workspaceID string, target dialer.QueueTarget) ([]string, error)
}

// EventSink records queue lifecycle events for reporting/SLA (Asterisk's
// queue_log). It is OFF the hot path, implementations MUST NOT block or fail the
// caller (fan out to a durable store asynchronously). A nil sink drops events.
type EventSink interface {
	QueueEvent(ev Event)
}

// Event is one queue lifecycle record.
type Event struct {
	At          time.Time
	WorkspaceID string
	TransferID  string
	CallID      string
	Target      dialer.QueueTarget
	Type        string // enqueued | connected | abandoned | overflow | queue_full | cancelled
	Position    int
	WaitedMS    int64
}

// Director implements dialer.QueueDirector. All state is in-memory and process
// local (the caller's live media leg is inherently pinned to this process, exactly
// like every other live-call structure here); a single mutex guards the ordered
// lines so the reaper Tick, presence-driven dequeue attempts and abandonment can
// all call in concurrently without a data race.
type Director struct {
	policy     PolicyResolver
	candidates CandidateSource
	events     EventSink
	logger     *log.Logger
	now        func() time.Time

	mu    sync.Mutex
	lines map[string][]*entry // lineKey -> FIFO order (head = index 0)
	byID  map[string]*entry   // transferID -> entry (O(1) lookup / idempotency)
}

type entry struct {
	caller  dialer.QueuedCaller
	lineKey string
}

// Config wires the director's collaborators. Candidates is required (there is no
// safe default for "who can take this call"); a nil PolicyResolver disables all
// queuing (every Policy is disabled); a nil EventSink drops events.
type Config struct {
	Policy     PolicyResolver
	Candidates CandidateSource
	Events     EventSink
	Logger     *log.Logger
	Now        func() time.Time
}

func New(cfg Config) *Director {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Director{
		policy:     cfg.Policy,
		candidates: cfg.Candidates,
		events:     cfg.Events,
		logger:     logger,
		now:        now,
		lines:      make(map[string][]*entry),
		byID:       make(map[string]*entry),
	}
}

var _ dialer.QueueDirector = (*Director)(nil)

// Policy resolves the normalized queue policy for a target. With no resolver wired,
// queuing is disabled.
func (d *Director) Policy(ctx context.Context, workspaceID string, target dialer.QueueTarget) dialer.QueuePolicy {
	if d == nil || d.policy == nil {
		return dialer.QueuePolicy{Enabled: false}
	}
	return d.policy.Resolve(ctx, workspaceID, target).Normalized()
}

// Admit records a caller at the TAIL of its line and returns the 1-based position.
// Idempotent per TransferID (re-admit returns the existing position). Returns
// dialer.ErrQueueFull when the line already holds MaxLength callers.
func (d *Director) Admit(ctx context.Context, caller dialer.QueuedCaller, policy dialer.QueuePolicy) (int, error) {
	if d == nil {
		return 0, dialer.ErrQueueFull
	}
	if err := caller.Target.Validate(); err != nil {
		return 0, err
	}
	policy = policy.Normalized()
	lineKey := caller.Target.Key(caller.WorkspaceID)

	d.mu.Lock()
	// Idempotent: an already-queued caller keeps its place.
	if e, ok := d.byID[caller.TransferID]; ok {
		pos := d.positionLocked(e)
		d.mu.Unlock()
		return pos, nil
	}
	if len(d.lines[lineKey]) >= policy.MaxLength {
		d.mu.Unlock()
		d.emit(Event{
			At: d.now(), WorkspaceID: caller.WorkspaceID, TransferID: caller.TransferID,
			CallID: caller.CallID, Target: caller.Target, Type: dialer.QueueReasonFull,
		})
		return 0, dialer.ErrQueueFull
	}
	e := &entry{caller: caller, lineKey: lineKey}
	d.lines[lineKey] = append(d.lines[lineKey], e)
	d.byID[caller.TransferID] = e
	pos := len(d.lines[lineKey])
	d.mu.Unlock()

	d.emit(Event{
		At: d.now(), WorkspaceID: caller.WorkspaceID, TransferID: caller.TransferID,
		CallID: caller.CallID, Target: caller.Target, Type: "enqueued", Position: pos,
	})
	return pos, nil
}

// Candidates returns the agents currently available for a queued caller's target.
// Pure passthrough to the candidate source (reservation-aware), so no lock is held
// while the engine rings.
func (d *Director) Candidates(ctx context.Context, caller dialer.QueuedCaller) ([]string, error) {
	if d == nil || d.candidates == nil {
		return nil, nil
	}
	return d.candidates.AvailableForTarget(ctx, caller.WorkspaceID, caller.Target)
}

// Position returns the caller's current 1-based position (0 when not queued).
func (d *Director) Position(workspaceID, transferID string) int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.byID[transferID]
	if !ok {
		return 0
	}
	return d.positionLocked(e)
}

// Remove drops a caller from its line and records the outcome. Idempotent.
func (d *Director) Remove(ctx context.Context, workspaceID, transferID, reason string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	e, ok := d.byID[transferID]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.byID, transferID)
	ln := d.lines[e.lineKey]
	for i, cur := range ln {
		if cur == e {
			ln = append(ln[:i], ln[i+1:]...)
			break
		}
	}
	if len(ln) == 0 {
		delete(d.lines, e.lineKey)
	} else {
		d.lines[e.lineKey] = ln
	}
	waited := int64(0)
	if !e.caller.EnqueuedAt.IsZero() {
		waited = d.now().Sub(e.caller.EnqueuedAt).Milliseconds()
	}
	d.mu.Unlock()

	if reason == "" {
		reason = dialer.QueueReasonCancelled
	}
	d.emit(Event{
		At: d.now(), WorkspaceID: e.caller.WorkspaceID, TransferID: transferID,
		CallID: e.caller.CallID, Target: e.caller.Target, Type: reason, WaitedMS: waited,
	})
}

// Len reports how many callers are waiting in a line (used by tests and metrics).
func (d *Director) Len(workspaceID string, target dialer.QueueTarget) int {
	if d == nil {
		return 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.lines[target.Key(workspaceID)])
}

func (d *Director) positionLocked(e *entry) int {
	for i, cur := range d.lines[e.lineKey] {
		if cur == e {
			return i + 1
		}
	}
	return 0
}

func (d *Director) emit(ev Event) {
	if d == nil || d.events == nil {
		return
	}
	d.events.QueueEvent(ev)
}
