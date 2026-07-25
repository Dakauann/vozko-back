package dialer

import (
	"context"
	"errors"
	"strings"
	"time"
)

// This file is the CONTROL PLANE of the call queue (ACD): the target selector, the
// bounds that guarantee no caller ever waits forever, and the QueueDirector port
// the transfer engine consults. It carries no media/SIP knowledge. A queued caller
// is a parked leg (owned by the transfer engine, exactly like a recall) whose ring
// target is a pool of agents instead of the original initiator; see
// docs/CALL_TRANSFER_PRODUCTION_PLAN.md.

// --- Target selector -------------------------------------------------------

// QueueTargetKind selects WHO a queued caller waits for. Departments are NOT
// required: agent and workspace lines exist so a workspace with no departments can
// still queue.
type QueueTargetKind string

const (
	// QueueTargetDepartment waits for any available member of a department (dequeue
	// resolves candidates via the roulette's department scoping).
	QueueTargetDepartment QueueTargetKind = "department"
	// QueueTargetAgent waits for one specific agent to free up (e.g. a blind transfer
	// to a busy colleague).
	QueueTargetAgent QueueTargetKind = "agent"
	// QueueTargetWorkspace is the general line: any available agent in the workspace.
	// This is the roulette with the department filter off.
	QueueTargetWorkspace QueueTargetKind = "workspace"
)

func (k QueueTargetKind) Valid() bool {
	switch k {
	case QueueTargetDepartment, QueueTargetAgent, QueueTargetWorkspace:
		return true
	}
	return false
}

// QueueTarget names who a queued caller waits for. Department and Agent carry an
// ID; Workspace carries none (any available agent).
type QueueTarget struct {
	Kind QueueTargetKind `json:"kind"`
	ID   string          `json:"id,omitempty"`
}

func (t QueueTarget) Validate() error {
	if !t.Kind.Valid() {
		return ErrQueueTargetInvalid
	}
	switch t.Kind {
	case QueueTargetDepartment, QueueTargetAgent:
		if strings.TrimSpace(t.ID) == "" {
			return ErrQueueTargetInvalid
		}
	}
	return nil
}

// Key is the stable identity of a waiting line within a workspace, so every caller
// waiting for the SAME target shares one ordered line (which is what makes position
// meaningful).
func (t QueueTarget) Key(workspaceID string) string {
	return workspaceID + "|" + string(t.Kind) + "|" + t.ID
}

// --- Bounds (the "never wait forever" guarantee) ---------------------------

// QueueOverflowAction is what happens to a caller who reaches the hard wait ceiling
// (MaxWait) or arrives at a full line.
type QueueOverflowAction string

const (
	// QueueOverflowHangup plays the configured fallback announcement then hangs the
	// caller up. It reuses the transfer engine's park fallback (AbandonPark), so it
	// is the safe default: the caller is never left in infinite hold audio.
	QueueOverflowHangup QueueOverflowAction = "hangup"
	// QueueOverflowRecall falls back to recalling the ORIGINAL initiator. Only
	// meaningful for a human-initiated transfer; with no initiator it degrades to
	// hangup.
	QueueOverflowRecall QueueOverflowAction = "recall"
)

func (a QueueOverflowAction) Valid() bool {
	switch a {
	case QueueOverflowHangup, QueueOverflowRecall:
		return true
	}
	return false
}

const (
	DefaultQueueMaxWait   = 5 * time.Minute
	DefaultQueueMaxLength = 25
	// Hard caps so a misconfigured policy can never produce an unbounded wait or an
	// unbounded number of channels/MOH streams held open.
	QueueMaxWaitCap   = 30 * time.Minute
	QueueMaxLengthCap = 200
)

// QueueStrategy is how a waiting line rings its candidates, mirroring Asterisk's
// app_queue strategies. Only the two the product needs today:
type QueueStrategy string

const (
	// QueueStrategyRRMemory rings ONE available candidate at a time, round-robin with
	// memory, HOLDING the caller (MOH) up to MaxWait for an agent to free up. This is
	// the ACD hold behaviour (Asterisk "rrmemory").
	QueueStrategyRRMemory QueueStrategy = "rrmemory"
	// QueueStrategyRingAll rings ALL currently-available candidates at once in a single
	// pass and overflows if none answer within the ring window. No hold, no re-ring:
	// the "ring the team, no queue" behaviour (Asterisk "ringall" + short timeout).
	QueueStrategyRingAll QueueStrategy = "ringall"
)

func (s QueueStrategy) Valid() bool {
	switch s {
	case QueueStrategyRRMemory, QueueStrategyRingAll:
		return true
	}
	return false
}

// QueuePolicy bounds a waiting line so a caller is ALWAYS eventually terminated:
// MaxWait is a hard ceiling (reuses the transfer handle's ParkDeadline), MaxLength
// caps simultaneous callers (bounds channel/MOH consumption, which ties into the
// channel-addon billing), Strategy is the ring pattern, and Overflow is the terminal
// action. Enabled=false means "do not queue" and the engine keeps its non-queue
// behavior (e.g. camp-on to a busy agent returns busy synchronously).
type QueuePolicy struct {
	Enabled   bool                `json:"enabled"`
	Strategy  QueueStrategy       `json:"strategy"`
	MaxWait   time.Duration       `json:"maxWait"`
	MaxLength int                 `json:"maxLength"`
	Overflow  QueueOverflowAction `json:"overflow"`
}

// Holds reports whether this policy makes a caller WAIT for an agent (rrmemory hold)
// vs ring the currently-available pool once and overflow (ringall, no hold).
func (p QueuePolicy) Holds() bool {
	return p.Strategy != QueueStrategyRingAll
}

// Normalized clamps the policy to safe defaults and hard caps so a zero or
// misconfigured policy can never produce an unbounded wait or line. It does NOT
// force Enabled: a disabled policy stays disabled.
func (p QueuePolicy) Normalized() QueuePolicy {
	out := p
	if !out.Strategy.Valid() {
		out.Strategy = QueueStrategyRRMemory
	}
	if out.MaxWait <= 0 {
		out.MaxWait = DefaultQueueMaxWait
	}
	if out.MaxWait > QueueMaxWaitCap {
		out.MaxWait = QueueMaxWaitCap
	}
	if out.MaxLength <= 0 {
		out.MaxLength = DefaultQueueMaxLength
	}
	if out.MaxLength > QueueMaxLengthCap {
		out.MaxLength = QueueMaxLengthCap
	}
	if !out.Overflow.Valid() {
		out.Overflow = QueueOverflowHangup
	}
	return out
}

// --- Director port (policy brain; owns the line, never touches media) --------

// QueuedCaller is the transfer engine's view of a caller waiting in line, handed to
// the QueueDirector for admission, candidate resolution and removal.
type QueuedCaller struct {
	TransferID  string
	WorkspaceID string
	CallID      string
	Target      QueueTarget
	Phone       string
	EnqueuedAt  time.Time
}

// QueueDirector is the policy brain the transfer engine consults to run a queue. It
// owns the ordered waiting line (position), the target -> candidate resolution, the
// bounds and the event log. The transfer engine owns the MEDIA (the parked leg, the
// ring waves, the attach); it calls these hooks but the director never touches
// media, so the two layers stay cleanly separated and independently testable.
//
// All methods are safe for concurrent use: the reaper Tick, presence-driven
// dequeue attempts and abandonment can all call in at once.
type QueueDirector interface {
	// Policy resolves the (already Normalized) queue policy for a target. A policy
	// with Enabled=false means "do not queue".
	Policy(ctx context.Context, workspaceID string, target QueueTarget) QueuePolicy

	// Admit records a newly enqueued caller at the TAIL of its line and returns the
	// caller's 1-based position. It returns ErrQueueFull when the line already holds
	// MaxLength callers (the engine then runs overflow immediately, without parking).
	// Admit is idempotent per TransferID: re-admitting an already-queued caller
	// returns its existing position.
	Admit(ctx context.Context, entry QueuedCaller, policy QueuePolicy) (position int, err error)

	// Candidates returns the agent user ids to ring in the next wave for a queued
	// caller: the pool currently available for the caller's target. Empty means
	// "nobody free right now" and the engine simply waits for the next tick / presence
	// change. Never returns the caller's own initiator.
	Candidates(ctx context.Context, entry QueuedCaller) ([]string, error)

	// Position returns the caller's current 1-based position in its line (0 when not
	// queued). Cheap; safe to poll for announcements.
	Position(workspaceID, transferID string) int

	// Remove drops the caller from its line (connected / abandoned / overflowed) and
	// records the outcome. Idempotent: removing an unknown/already-removed caller is a
	// no-op.
	Remove(ctx context.Context, workspaceID, transferID, reason string)
}

// Queue outcome reasons (recorded via QueueDirector.Remove and emitted to the event
// log). Kept alongside TransferReason so the two vocabularies live together.
const (
	QueueReasonConnected = "connected"  // a wave was accepted; caller bridged to an agent
	QueueReasonAbandoned = "abandoned"  // caller hung up while waiting
	QueueReasonOverflow  = "overflow"   // hit MaxWait; ran the overflow action
	QueueReasonFull      = "queue_full" // arrived at a full line
	QueueReasonCancelled = "cancelled"  // torn down (e.g. initiator cancelled)
)

// EnqueueRequest asks the transfer engine to park a caller and place them in the
// ACD queue for a target. Used by surfaces that could not connect an agent
// immediately: the roulette on exhaustion, the AI transfer tools on busy, and (soon)
// inbound routing with no free agent. InitiatorID is the AI owner id for an
// AI-owned call (the leg is surrendered + parked) or a human user id (the leg is
// detached from their browser + parked).
type EnqueueRequest struct {
	WorkspaceID string
	CallID      string
	InitiatorID string
	Target      QueueTarget
	Note        string
}

func (r EnqueueRequest) Validate() error {
	if r.WorkspaceID == "" {
		return ErrWorkspaceRequired
	}
	if r.CallID == "" {
		return ErrTransferCallNotFound
	}
	return r.Target.Validate()
}

var (
	ErrQueueTargetInvalid = errors.New("dialer queue: invalid target")
	ErrQueueFull          = errors.New("dialer queue: line is full")
	// ErrQueueDisabled means no queue policy is enabled for the target (or the
	// engine has no director wired); the caller keeps its non-queue behavior.
	ErrQueueDisabled = errors.New("dialer queue: not enabled for target")
)
