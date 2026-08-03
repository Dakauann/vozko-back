package dialer

// ParkedLeg is a live caller leg that is attached to NO session: detached from the
// initiator, kept alive (lifecycle, billing, recording continue) and fed hold audio
// while a transfer resolves. It is the equivalent of Asterisk's transferee bridge,
// the thing Vozko lacked: without it a leg only exists attached to a session or
// inside an in-flight executor call, which is why blind transfers had to hold the
// initiator hostage until the target answered.
//
// OwnerToken is the transfer ID that parked the leg. Retrieve/Abandon are token
// scoped compare-and-set, the same discipline as the ring ReservationState, so a
// recall attach racing a park-deadline fallback resolves to exactly one winner.
type ParkedLeg struct {
	CallID      string
	WorkspaceID string
	OwnerToken  string
	Leg         CallLeg
}

// ParkRegistry owns parked legs. Implementations must watch each leg's Done
// channel and fire onDeath exactly once if the leg dies while still parked (the
// caller hung up in the hold/ring window), that callback is the transfer engine's
// customer-hangup funnel. A leg that was retrieved or abandoned must NOT fire it.
type ParkRegistry interface {
	// Park stores the leg under (workspaceID, leg.CallID()) owned by ownerToken.
	// Fails if a different leg is already parked for that call.
	Park(workspaceID string, leg CallLeg, ownerToken string, onDeath func()) error
	// Retrieve removes and returns the leg iff ownerToken matches (CAS). The death
	// watcher is disarmed before the leg is handed back.
	Retrieve(workspaceID, callID, ownerToken string) (CallLeg, bool)
	// Abandon removes the leg iff ownerToken matches, without handing it to a new
	// owner (the fallback terminal: the caller could not be placed anywhere). The
	// death watcher is disarmed; hanging the leg up is the caller's job.
	Abandon(workspaceID, callID, ownerToken string) (CallLeg, bool)
	// Find peeks at the parked entry for a call, if any.
	Find(workspaceID, callID string) (ParkedLeg, bool)
}
