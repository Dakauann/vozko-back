package dialer

import "vozko/domain/voip"

// CallLeg is a media-agnostic handle to the in-progress call leg being moved
// between transfer targets. A browser target re-points its PCM audio forwarder to
// the leg (recovering the concrete leg by type assertion in its own package); a SIP
// target (a branch) takes the leg's raw media via SurrenderMedia and RTP-bridges it
// to the registered phone. Only what a SIP target needs crosses the domain boundary,
// so no delivery/infra media type leaks into the domain.
type CallLeg interface {
	CallID() string
	PhoneNumber() string
	// SurrenderMedia stops the leg's own RTP read/write loops and returns exclusive
	// raw access to its media session, leaving the SIP dialog up. After it returns
	// the caller is the sole reader/writer. It errors if the leg has no surrenderable
	// media (a WebSocket-only leg) or was already surrendered.
	SurrenderMedia() (voip.MediaSession, error)
	// Hangup ends the underlying call leg. A SIP target calls it when the phone hangs
	// up or the bridge tears down.
	Hangup() error
	// Done is closed when the leg's lifecycle ends (the far side hung up or the call
	// was torn down). The park registry watches it so a caller who hangs up while
	// parked/held aborts the in-flight transfer immediately instead of ghost-ringing
	// the target until the reaper.
	Done() <-chan struct{}
}

// CallLegSink is a transfer target that can receive a call leg (AttachLeg) or yield
// the one it currently holds (DetachLeg). The browser dialerSession implements it by
// re-pointing its audio forwarder; the branch session implements it by surrendering
// the leg's media and starting an RTP bridge to the registered phone. Generalizing
// the transfer executor against this port instead of the concrete browser session is
// what lets a branch be a first-class transfer target with NO duplication of the
// transfer FSM (plan §5.4). A terminal target (a branch) may return (nil,false) from
// DetachLeg since it never yields a leg back.
type CallLegSink interface {
	AttachLeg(leg CallLeg) error
	DetachLeg() (CallLeg, bool)
}
