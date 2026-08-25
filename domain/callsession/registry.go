package callsession

import (
	"vozko/domain/conversation"
)

type CallSessionControlMessage struct {
	Type    string
	Payload any
}

type CallSession interface {
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
	Notify(msg CallSessionControlMessage) error

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

type CallSessionRegistry interface {
	Register(s CallSession) (deregister func(), err error)

	// FindByUser returns a single preferred contact for callers that target one
	// endpoint (the most recently attached session).
	FindByUser(workspaceID, userID string) (CallSession, bool)

	// FindSessionsByUser returns ALL of a member's live contacts, so an offer can
	// fork to every one of them, first to answer wins. Empty when the member has no
	// session.
	FindSessionsByUser(workspaceID, userID string) []CallSession

	// FindByID resolves a live session by its session ID, across users. Used to cancel
	// the members of a ring wave whose offered contacts span several members and
	// cannot be found via a single user. Returns false when gone.
	FindByID(sessionID string) (CallSession, bool)

	ListAvailable(workspaceID string) []CallSession

	ListAll(workspaceID string) []CallSession

	// ListPresence returns one row per ONLINE member with their live status. This is
	// the source of truth the call session presence panel renders; offline members are the
	// workspace roster minus these. Busy = the member has no free contact (every
	// session is on a call or ringing).
	ListPresence(workspaceID string) []MemberPresence

	// ListBrowserSessions returns every browser (WebSocket) session in the workspace,
	// i.e. the sessions that can receive a pushed presence snapshot.
	ListBrowserSessions(workspaceID string) []CallSession

	SetPresenceListener(listener PresenceListener)

	NotifyPresenceChanged(workspaceID string)
}

// MemberPresence is one member's live call session presence. Offline is represented by
// absence from the list.
type MemberPresence struct {
	UserID     string
	Busy       bool // on call OR ringing (not free for routing)
	OnCall     bool // has an attached call on any session
	Ringing    bool // reserved/ringing only (no attached call yet)
	HasBrowser bool // a browser softphone session is connected
}

type PresenceListener interface {
	OnPresenceChanged(workspaceID string)
}

type CallEntry struct {
	CallID         string
	WorkspaceID    string
	OwnerSessionID string
	OwnerUserID    string

	Phone string
	Call  conversation.CRMCall
	Lease *CallAdmissionLease
}

type CallRegistry interface {
	Register(entry CallEntry) error
	Lookup(workspaceID, callID string) (CallEntry, bool)
	Unregister(workspaceID, callID string)
}
