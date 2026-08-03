package dialer

import (
	"context"
	"time"

	"vozko/domain/conversation"
)

type DialerControlMessage struct {
	Type    string
	Payload any
}

type DialerSession interface {
	ID() string
	UserID() string
	WorkspaceID() string
	// HasActiveCall reports whether the session is occupied for routing, either
	// it has an attached call OR a live ring reservation (an offer is ringing the
	// agent but has not been accepted yet). This is the single availability
	// predicate every routing path reads (directly and via ListAvailable), so an
	// agent whose phone is already ringing is never offered a second call.
	HasActiveCall() bool
	// ActiveCallID returns the id of the attached call, or "" when none is
	// attached (including when the session is merely reserved/ringing).
	ActiveCallID() string
	Notify(msg DialerControlMessage) error

	// Reserve marks the session as occupied for an outstanding ring identified by
	// token (the offer/transfer id). It is a compare-and-set: it returns false if
	// the session already has an attached call or a live reservation for a
	// different token, so two concurrent offers can never both claim the same idle
	// agent. Reserving again with the same token is idempotent (returns true).
	Reserve(token string) bool
	// Release clears a reservation taken with the same token. It is token-scoped
	// and idempotent: releasing a stale/foreign token (e.g. after the reservation
	// was already consumed by accept, or the agent reconnected and was re-reserved)
	// is a no-op, so duplicate/liberal releases are safe.
	Release(token string)
}

// RingEligibleSession is an OPTIONAL session capability: an endpoint that can be
// excluded from ringing per the member's ring-channel policy. A session that does
// not implement it is always eligible to ring. This is the single contract used by
// both the registry (offer/availability filtering) and the transfer fork, so the
// ring-eligibility check is defined in one place, not duplicated.
type RingEligibleSession interface {
	RingEligible() bool
}

type DialerSessionRegistry interface {
	Register(s DialerSession) (deregister func(), err error)

	// FindByUser returns a single preferred contact for callers that target one
	// endpoint (a registered branch wins over a transient browser session).
	FindByUser(workspaceID, userID string) (DialerSession, bool)

	// FindSessionsByUser returns ALL of a member's live contacts (browser + branch),
	// so a transfer offer can fork to every endpoint and ring them together, first to
	// answer wins. Empty when the member has no session.
	FindSessionsByUser(workspaceID, userID string) []DialerSession

	// FindByID resolves a live session by its session ID, across users. Used to cancel
	// the members of a multi-user ring wave (ringall), where the offered contacts span
	// several members and cannot be found via a single user. Returns false when gone.
	FindByID(sessionID string) (DialerSession, bool)

	ListAvailable(workspaceID string) []DialerSession

	ListAll(workspaceID string) []DialerSession

	// ListPresence returns one row per ONLINE member with their live status and which
	// endpoint kinds they hold (a browser softphone, a registered branch, or both). This
	// is the source of truth the dialer presence panel renders; offline members are the
	// workspace roster minus these. Busy = the member has no free contact (every
	// endpoint is on a call or ringing).
	ListPresence(workspaceID string) []MemberPresence

	// ListBrowserSessions returns every browser (WebSocket) session in the workspace,
	// i.e. the sessions that can receive a pushed presence snapshot. A branch is a SIP
	// phone with no WS channel, so it is excluded.
	ListBrowserSessions(workspaceID string) []DialerSession

	SetPresenceListener(listener PresenceListener)

	NotifyPresenceChanged(workspaceID string)
}

// MemberPresence is one member's live dialer presence: busy/available plus which
// endpoint kinds they currently hold. Offline is represented by absence from the list.
type MemberPresence struct {
	UserID     string
	Busy       bool // on call OR ringing (not free for routing)
	OnCall     bool // has an attached call on any endpoint
	Ringing    bool // reserved/ringing only (no attached call yet)
	HasBrowser bool // a browser softphone session is connected
	HasBranch  bool // a registered SIP extension (branch) is online
}

type PresenceListener interface {
	OnPresenceChanged(workspaceID string)
}

type DialerCallEntry struct {
	CallID         string
	WorkspaceID    string
	OwnerSessionID string
	OwnerUserID    string
	SIPTrunkID     string

	Phone string
	Call  conversation.CRMCall
	Lease *CallAdmissionLease
}

type DialerCallRegistry interface {
	Register(entry DialerCallEntry) error
	Lookup(workspaceID, callID string) (DialerCallEntry, bool)
	Rebind(workspaceID, callID, expectedOwnerSessionID, newOwnerSessionID, newOwnerUserID string) error
	Unregister(workspaceID, callID string)
}

type TransferStore interface {
	Put(h *TransferHandle) error
	Get(transferID string) (*TransferHandle, bool)

	Update(transferID string, mut func(*TransferHandle) error) (*TransferHandle, error)

	FindByCall(workspaceID, callID string) (*TransferHandle, bool)
	Delete(transferID string)

	ExpireBefore(cutoff time.Time) []*TransferHandle

	// ListByStage returns a snapshot of every handle currently in the given stage,
	// so the reaper Tick can drive time-based transitions (stale pending offers,
	// the recall ladder, park deadlines, wedged completing handles) through the
	// same Update CAS as every other actor.
	ListByStage(stage TransferStage) []*TransferHandle
}

type TransferExecutor interface {
	SwapBlind(ctx context.Context, h *TransferHandle) error
	OpenConsult(ctx context.Context, h *TransferHandle) error
	CloseConsult(ctx context.Context, h *TransferHandle, completed bool) error

	// HoldCaller puts the caller leg on hold the instant a transfer starts (hold
	// audio replaces both directions of the initiator<->caller bridge), so the
	// caller never hears the initiator's mic or dead air during the ring/consult
	// window. A no-op for AI-initiated handles (the AI leg manages its own audio).
	HoldCaller(ctx context.Context, h *TransferHandle) error
	// ReleaseHold resumes normal two-way audio between the caller and whoever the
	// leg is attached to. Idempotent; safe on legs that were never held.
	ReleaseHold(ctx context.Context, h *TransferHandle)

	// ParkBlind detaches the caller leg from the initiator, keeps it on hold audio
	// in the park registry (owned by h.ID) and frees the initiator: the true-blind
	// semantic. Also used when an attended transfer is completed while the target
	// is still ringing (blond). The initiator's UI is told its call ended with
	// reason "transferred".
	ParkBlind(ctx context.Context, h *TransferHandle) error
	// SwapParked retrieves the parked leg and attaches it to the contact that won
	// the CURRENT ring wave (AcceptedSessionID among toUserID's sessions). On
	// failure the leg is re-parked so the recall ladder can run.
	SwapParked(ctx context.Context, h *TransferHandle, toUserID string) error
	// AbandonPark is the fallback terminal for a parked leg nobody could take:
	// stop hold audio, play the configured fallback announcement (when present)
	// and hang the caller up.
	AbandonPark(ctx context.Context, h *TransferHandle, reason string)
}

type CallTransferUseCase interface {
	Initiate(ctx context.Context, req TransferRequest) (*TransferHandle, error)
	// Accept completes the offer for the contact identified by acceptingSessionID
	// (the browser session or the branch that answered). The first accept wins the
	// stage compare-and-set; a second accept from another contact fails, and every
	// other offered contact is cancelled so it stops ringing.
	Accept(ctx context.Context, transferID, targetUserID, acceptingSessionID string) (*TransferHandle, error)
	Decline(ctx context.Context, transferID, targetUserID, reason string) error
	CompleteAttended(ctx context.Context, transferID, initiatorID string) (*TransferHandle, error)
	CancelAttended(ctx context.Context, transferID, initiatorID, reason string) error

	AbortByDisconnect(ctx context.Context, transferID, reason string) error

	// AbortByCall aborts whatever offer is in flight for a call (if any), used when
	// the initiating agent hangs up or disconnects mid-transfer so every ringing
	// contact stops and their reservations are freed. A no-op when nothing is pending.
	AbortByCall(ctx context.Context, workspaceID, callID, reason string) error

	// AbortByCallLegDeath is the customer-hangup funnel: the caller's leg died
	// (lifecycle ended / parked leg watcher fired) while a transfer was in flight.
	// It terminalizes the handle as failed(customer_hangup), stops every ringing
	// contact instantly, closes any consult notifying BOTH agents, and abandons the
	// park entry. A no-op when nothing is in flight.
	AbortByCallLegDeath(ctx context.Context, workspaceID, callID string) error

	// FindActiveByCall returns the non-terminal transfer in flight for a call, if
	// any, so delivery can make stage-aware decisions (e.g. the initiator's EndCall
	// during a consult cancels the consult instead of killing the customer's call).
	FindActiveByCall(workspaceID, callID string) (*TransferHandle, bool)

	ReapExpiredOffers(ctx context.Context, cutoff time.Time) int

	// Tick drives every time-based transition: expiring unanswered offers (which
	// recalls parked-blind handles instead of terminalizing them), advancing the
	// recall ladder, enforcing park deadlines, and failing wedged completing
	// handles. Called by the container reaper loop. Returns how many handles it
	// transitioned.
	Tick(ctx context.Context, now time.Time) int
}
