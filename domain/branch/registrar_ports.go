package branch

import "vozko/domain/voip"

// BranchStatusUpdater is the narrow slice of the repository the registrar uses to
// reflect live SIP presence on the branch row (registered on REGISTER, unreachable
// when the reaper evicts the last contact) so the dashboard shows reality.
type BranchStatusUpdater interface {
	UpdateRegistrationStatus(id string, status RegistrationStatus) error
}

// BranchMediaBridge connects the branch's currently-answered phone dialog to
// either the transferred caller (StartBridge, a blind transfer / attended
// completion) or the initiating agent (StartConsultBridge, the private consult
// leg of an attended transfer). It is implemented by the SIP registrar (which
// owns the answered dialog, the RTP relay and the G.711 codec) and injected into
// the branch dialer session, so the session stays free of the SIP/RTP library.
type BranchMediaBridge interface {
	// StartBridge relays the surrendered caller media to the branch's answered
	// phone leg (parked when the phone answered). It is called from the branch
	// session's AttachLeg inside the transfer executor's swap (blind transfer, or
	// completing an attended transfer). onEnd fires once when the bridge ends on
	// its own (the phone hangs up), so the session can tear the caller leg down;
	// the returned stop tears the bridge down from the caller side (idempotent,
	// safe after onEnd). Errors if the branch has no answered dialog.
	StartBridge(branchID, callID string, caller voip.MediaSession, onEnd func()) (stop func(), err error)

	// StartConsultBridge relays the answered phone leg to a PCM consult peer (the
	// initiating agent's browser, via the consult bridge), transcoding G.711<->PCM.
	// It is called from the branch session's AttachConsult when an ATTENDED
	// transfer target answers: the phone talks privately to the agent while the
	// caller waits on hold. The answered phone leg is KEPT parked so a subsequent
	// StartBridge (attended completion) can bridge it to the caller. onPhoneGone
	// fires once if the phone hangs up during the consult (target-disconnect abort).
	// The returned stop ends the consult relay synchronously WITHOUT hanging up the
	// phone (completion reuses the phone leg; CancelConsult hangs it up).
	StartConsultBridge(branchID string, peer ConsultPeer, onPhoneGone func()) (stop func(), err error)

	// CancelConsult hangs up the parked phone leg after a consult was cancelled
	// (the agent chose not to complete the attended transfer). Idempotent; a no-op
	// if the leg was already consumed by StartBridge (completion).
	CancelConsult(branchID string)
}

// ConsultPeer is the PCM duplex the branch consult relay talks to: the initiating
// agent's end of the attended-transfer consult bridge. Send carries audio decoded
// from the phone toward the agent; Recv yields the agent's audio to encode toward
// the phone. The consult endpoint (infra/dialer) satisfies it structurally, so no
// delivery/infra media type crosses into this domain port.
type ConsultPeer interface {
	Send(pcm16 []byte)
	Recv() <-chan []byte
}

// BranchRingRequest describes an offered call being rung toward a registered
// branch. The infra implementation places a SIP INVITE to the branch's live
// contacts and, on answer, bridges media; on decline/timeout it fails.
type BranchRingRequest struct {
	BranchID    string
	SIPUser     string
	WorkspaceID string
	UserID      string
	CallID      string // the call being offered/transferred
	TransferID  string // the transfer handle, if this ring is a transfer offer
	InitiatorID string // who initiated the transfer, so a failed forward hunt returns the caller to them
}

// BranchRing rings a registered branch. It is injected into the branch dialer
// session so the session (and its unit tests) stay free of the SIP library.
// Ring returns quickly once the INVITE is on its way (or an error if it could not
// be started); the answer/decline is resolved asynchronously by the SIP dialog.
type BranchRing interface {
	Ring(req BranchRingRequest) error
	// CancelRing stops an in-flight ring (sends SIP CANCEL) when the offered call was
	// answered on another contact or declined, so the phone stops ringing at once
	// instead of ringing out. A no-op if the branch is not currently ringing that call.
	CancelRing(req BranchRingRequest) error
}

// RegisteredBranch is the identity handed to presence when a phone comes online.
type RegisteredBranch struct {
	BranchID    string
	SIPUser     string
	WorkspaceID string
	UserID      string
}

// BranchPresenceListener is notified as branches become reachable / unreachable so
// a branch dialer session can be added to or removed from the routing registry.
// This is what makes a registered phone a transfer target: on OnBranchReachable a
// DialerSession is registered under the branch's user, so the existing transfer
// engine's FindByUser resolves it with no new transfer logic.
type BranchPresenceListener interface {
	OnBranchReachable(b RegisteredBranch)
	OnBranchUnreachable(branchID string)
}
