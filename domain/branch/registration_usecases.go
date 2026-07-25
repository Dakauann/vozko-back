package branch

// RegisterRequest is the SIP-wire-independent view of a REGISTER the infra
// adapter passes to the use case. The adapter (sipgo) parses the request and
// fills this; the use case never sees a sip.Request, keeping the domain free of
// the SIP library.
type RegisterRequest struct {
	SIPUser string
	HasAuth bool

	// Authorization fields (present only when HasAuth).
	Realm    string
	Nonce    string
	URI      string
	Response string
	QOP      string
	NC       string
	CNonce   string
	Method   string

	// Binding fields.
	Contact          string
	ReceivedFrom     string // real UDP source ip:port (rport), the rewrite_contact target
	Transport        string
	CallID           string
	CSeq             int
	UserAgent        string
	RequestedExpires int
}

// RegisterAction is the outcome the adapter turns into a SIP response.
type RegisterAction string

const (
	RegisterChallenge        RegisterAction = "challenge"          // 401 + WWW-Authenticate
	RegisterOK               RegisterAction = "ok"                 // 200 OK, binding stored
	RegisterDeregistered     RegisterAction = "deregistered"       // 200 OK, Expires: 0
	RegisterUnauthorized     RegisterAction = "unauthorized"       // 401, bad credentials
	RegisterNotFound         RegisterAction = "not_found"          // 404, no such branch
	RegisterForbidden        RegisterAction = "forbidden"          // 403, branch disabled
	RegisterIntervalTooBrief RegisterAction = "interval_too_brief" // 423 + Min-Expires
)

type RegisterResult struct {
	Action         RegisterAction
	Realm          string // challenge realm (must match the realm HA1 was derived under)
	Nonce          string // challenge nonce
	Stale          bool   // challenge: nonce was valid but expired -> phone retries without re-prompting
	GrantedExpires int    // OK: the clamped expiry echoed to the phone
	MinExpires     int    // IntervalTooBrief: the floor
}

// HandleRegisterUseCase authenticates a REGISTER against the branch HA1 and
// upserts/removes the AOR binding. It is fully unit-testable with fake ports.
type HandleRegisterUseCase interface {
	Execute(req RegisterRequest) (RegisterResult, error)
}

// RouteReason is why a routing attempt to a branch resolved the way it did.
type RouteReason string

const (
	RouteOK             RouteReason = "ok"
	RouteBranchNotFound RouteReason = "not_found"
	RouteDisabled       RouteReason = "disabled"
	RouteDND            RouteReason = "dnd"
	RouteOffline        RouteReason = "offline" // registered branch, but no live contact
)

// RouteResult carries the contacts to fork an INVITE to, or the reason none
// were returned (which drives the branch forward policy / transfer no-answer).
type RouteResult struct {
	Reason   RouteReason
	Branch   *Branch
	Contacts []RegistrationBinding
}

// RouteToBranchUseCase resolves a branch to its live contacts for ringing. Busy /
// double-ring is handled separately by the DialerSession Reserve/Release gate on
// the branch session, not here.
type RouteToBranchUseCase interface {
	Execute(workspaceID, sipUser string) (RouteResult, error)
}
