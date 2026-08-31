package inbox_assignment

import "time"

// RosterProvider lists roulette-eligible members REGARDLESS of connection
// state.
//
// It is the last-seen mode's counterpart to conversation.EligibleUserProvider,
// and it is a separate port on purpose: the WS hub can only answer for users it
// holds a socket for, which is precisely the constraint this mode exists to
// remove. Both pools filter with CanReceiveRoulette, so the two can never
// disagree about who is allowed to receive a conversation.
type RosterProvider interface {
	ListRouletteMembers(workspaceID, departmentID string, skipAdmins bool) ([]string, error)
}

// LastSeenReader is the presence slice the roulette needs. Satisfied by
// infra/repositories/agent_presence.
type LastSeenReader interface {
	// LastSeen returns, per user, the last moment they were present in the
	// workspace. Users with no presence history are ABSENT from the map rather
	// than zero-valued, so "never seen" stays distinguishable from "seen at the
	// zero time".
	LastSeen(workspaceID string, userIDs []string) (map[string]time.Time, error)
}

// EntryAttentionReader answers "did anyone actually work this conversation
// after it was handed over".
//
// Channel-agnostic, because conversation_messages is a single table keyed by
// (entry_id, entry_type) for every channel — the same reason the roulette
// itself needs no per-channel branch.
type EntryAttentionReader interface {
	AttendedSince(entryID, entryType, assignedUserID string, since time.Time) (bool, error)
}
