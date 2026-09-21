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
	HasActiveCall() bool
	ActiveCallID() string
	Notify(msg CallSessionControlMessage) error

	Reserve(token string) bool
	Release(token string)
}

type CallSessionRegistry interface {
	Register(s CallSession) (deregister func(), err error)

	FindByUser(workspaceID, userID string) (CallSession, bool)

	FindSessionsByUser(workspaceID, userID string) []CallSession

	FindByID(sessionID string) (CallSession, bool)

	ListAvailable(workspaceID string) []CallSession

	ListAll(workspaceID string) []CallSession

	ListPresence(workspaceID string) []MemberPresence

	ListBrowserSessions(workspaceID string) []CallSession

	SetPresenceListener(listener PresenceListener)

	NotifyPresenceChanged(workspaceID string)
}

type MemberPresence struct {
	UserID     string
	Busy       bool
	OnCall     bool
	Ringing    bool
	HasBrowser bool
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
