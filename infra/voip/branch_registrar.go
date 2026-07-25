package voipinfra

import (
	"context"
	"fmt"
	"log"
	"net"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"

	"vozko/domain/branch"
	"vozko/domain/cache"
	dialer_domain "vozko/domain/dialer"
	"vozko/domain/voip"
)

// BranchRegistrar is the single-VPS SIP registrar for branches. It owns one fixed
// public UDP port that phones REGISTER to, authenticates them (HA1, via the
// domain use case), keeps the AOR bindings, and originates INVITEs to ring a
// registered phone. It reuses the same diago/sipgo/pion stack as SIPTrunkManager
// and the media helpers in this package (WrapMediaSessionForLatching,
// mediaSessionAdapter, buildMediaInfo). It implements branch.BranchRing so the
// branch dialer session can ring a phone on a transfer offer.
//
// NOTE: the two-way media bridge that COMPLETES a transfer (connecting the branch
// leg to the party being transferred) is the one part that must be verified
// against live SIP/RTP; placeInvite rings the phone and answers, and the bridge
// hook is marked below.
type BranchRegistrar struct {
	cfg      BranchRegistrarConfig
	handle   branch.HandleRegisterUseCase
	store    branch.BindingStore
	route    branch.RouteToBranchUseCase
	presence branch.BranchPresenceListener
	logger   *log.Logger

	// limiter throttles REGISTER attempts per source IP (nil = unlimited). It is the
	// app-layer anti-flood / anti-brute-force guard on this public, pre-auth UDP port.
	limiter cache.RateLimiter

	// drainOnce guards the graceful shutdown drain so Stop is idempotent.
	drainOnce sync.Once

	ua     *sipgo.UserAgent
	server *sipgo.Server
	dg     *diago.Diago
	client *sipgo.Client // sends OPTIONS qualify probes to registered contacts

	// probe reports whether a contact answered an OPTIONS (reachable). Defaults to
	// probeOPTIONS; overridable in tests to exercise the eviction logic without SIP.
	probe func(recipient sip.Uri) bool

	// qualifyMisses counts consecutive OPTIONS failures per contact (sip_user|call_id);
	// a contact is evicted after qualifyMaxMisses so a dead phone drops faster than its
	// registration TTL. A successful probe clears the entry.
	qualifyMu     sync.Mutex
	qualifyMisses map[string]int

	// transferUC drives the reused transfer FSM once a phone answers (Accept ->
	// executor.SwapBlind -> branchSession.AttachLeg -> StartBridge). Injected after
	// initDialerTransferStack via SetTransferUseCase.
	transferUC dialer_domain.CallTransferUseCase

	// status reflects live SIP presence on the branch row (nil-safe).
	status branch.BranchStatusUpdater

	ctx    context.Context
	cancel context.CancelFunc

	dialogsMu sync.Mutex
	dialogs   map[string]*diago.DialogClientSession

	// answered holds a branch's just-answered phone leg between "phone 200 OK" and
	// the synchronous StartBridge call that consumes it (keyed by branchID; one live
	// ring per branch is guaranteed by the reservation gate).
	answeredMu sync.Mutex
	answered   map[string]*answeredBranch

	// pending tracks in-flight rings (INVITEs not yet answered) so CancelRing can
	// send a SIP CANCEL and stop the phone when the offer is answered on another
	// contact or declined. Keyed by branchID (one live ring per branch, gated by the
	// reservation).
	pendingMu sync.Mutex
	pending   map[string]*pendingRing
}

// pendingRing is an in-flight ring the registrar can cancel. peerCancelled marks a
// cancellation that came from the transfer engine (answered/declined elsewhere), so
// placeInvite tears down quietly instead of declining the transfer back.
type pendingRing struct {
	cancel        context.CancelFunc
	peerCancelled atomic.Bool
}

// answeredBranch is a branch's answered phone leg waiting to be bridged to the caller.
type answeredBranch struct {
	media  voip.MediaSession
	hangup func()
}

// defaultBranchRingSeconds is how long a branch rings before the ring is given up
// and the transfer is declined back. Matches the typical Asterisk/FreePBX extension
// ring time.
const defaultBranchRingSeconds = 30

type BranchRegistrarConfig struct {
	PublicHost   string // reachable public IP advertised to phones
	ListenPort   int    // fixed public UDP port phones register to
	Realm        string // digest realm (must match the realm HA1 was derived under)
	ReapInterval time.Duration

	// RTPPortStart/End is the media port window branch legs draw from. diago picks RTP
	// ports from the package-global media.RTPPortStart/End; the trunk manager normally
	// sets those, but if it did not run (disabled/failed/reordered) they are 0 and diago
	// silently binds OS ephemeral ports OUTSIDE the firewall RTP window -> media
	// blackhole. We pass the same window here so the registrar can guarantee a valid
	// range and refuse to start rather than blackhole silently.
	RTPPortStart int
	RTPPortEnd   int
}

func NewBranchRegistrar(
	cfg BranchRegistrarConfig,
	handle branch.HandleRegisterUseCase,
	store branch.BindingStore,
	route branch.RouteToBranchUseCase,
	presence branch.BranchPresenceListener,
	logger *log.Logger,
) *BranchRegistrar {
	if logger == nil {
		logger = log.Default()
	}
	if cfg.ReapInterval <= 0 {
		cfg.ReapInterval = 15 * time.Second
	}
	reg := &BranchRegistrar{
		cfg: cfg, handle: handle, store: store, route: route, presence: presence,
		logger: logger, dialogs: map[string]*diago.DialogClientSession{},
		answered:      map[string]*answeredBranch{},
		pending:       map[string]*pendingRing{},
		qualifyMisses: map[string]int{},
	}
	reg.probe = reg.probeOPTIONS
	return reg
}

var (
	_ branch.BranchRing        = (*BranchRegistrar)(nil)
	_ branch.BranchMediaBridge = (*BranchRegistrar)(nil)
)

// SetTransferUseCase injects the transfer engine used to complete a ring into a
// full transfer once the phone answers. Call before Start.
func (r *BranchRegistrar) SetTransferUseCase(uc dialer_domain.CallTransferUseCase) {
	r.transferUC = uc
}

// SetStatusUpdater injects the store used to reflect live SIP presence on the branch
// row. Call before Start. Nil-safe (presence is still tracked in memory without it).
func (r *BranchRegistrar) SetStatusUpdater(s branch.BranchStatusUpdater) {
	r.status = s
}

// Wire injects the runtime dependencies that form a cycle with the registrar
// (the registrar is itself the BranchRing the presence registrar rings, and the
// register use case notifies presence). Call before Start.
func (r *BranchRegistrar) Wire(handle branch.HandleRegisterUseCase, presence branch.BranchPresenceListener) {
	r.handle = handle
	r.presence = presence
}

// Start binds the registrar to its public port and begins serving. It reuses the
// diago transport setup from the trunk manager, but on a FIXED port with a pinned
// public host (STUN is unnecessary on a real public IP).
func (r *BranchRegistrar) Start(parent context.Context) error {
	if strings.TrimSpace(r.cfg.PublicHost) == "" {
		return fmt.Errorf("branch registrar: PublicSIPHost is required")
	}
	if r.handle == nil {
		return fmt.Errorf("branch registrar: register use case not wired")
	}
	r.ctx, r.cancel = context.WithCancel(parent)

	ua, err := sipgo.NewUA(
		sipgo.WithUserAgentHostname(r.cfg.PublicHost),
		sipgo.WithUserAgent("VozkoRegistrar"),
	)
	if err != nil {
		return fmt.Errorf("branch registrar: user agent: %w", err)
	}
	r.ua = ua

	srv, err := sipgo.NewServer(ua)
	if err != nil {
		return fmt.Errorf("branch registrar: server: %w", err)
	}
	srv.OnRegister(r.recovered("REGISTER", r.onRegister))
	srv.OnRequest(sip.OPTIONS, r.recovered("OPTIONS", r.onOptions))
	r.server = srv

	// Bind SIP + RTP to a CONCRETE routable interface, never 0.0.0.0. diago derives
	// the media (RTP) socket bind IP from BindHost; when BindHost is unspecified it
	// falls back to sip.ResolveInterfacesIP, which on multi-adapter hosts (Windows
	// APIPA, VPN/Hyper-V/WSL virtual NICs) can pick a dead link-local 169.254.x.x
	// address. Outbound RTP from there fails with "unreachable network" and the phone
	// can never reach us either, so the transfer bridges with zero audio both ways.
	// Pinning the bind to the real outbound interface (as SIPTrunkManager does) is what
	// actually makes media flow. MediaExternalIP only rewrites the advertised SDP host,
	// not the socket, so it is not enough on its own.
	bindHost, advertiseHost := r.resolveMediaBind()
	transport := diago.Transport{
		Transport:    "udp4",
		BindHost:     bindHost,
		BindPort:     r.cfg.ListenPort,
		ExternalHost: advertiseHost,
		ExternalPort: r.cfg.ListenPort,
	}
	if ip := net.ParseIP(advertiseHost); ip != nil {
		transport.MediaExternalIP = ip
	}
	r.logger.Printf("[branch-registrar] media bind=%s advertise=%s (PublicHost=%q)", bindHost, advertiseHost, r.cfg.PublicHost)

	// Offer PCMA (G.711 a-law) first, matching the Brazilian SIP trunks, so the RTP
	// bridge relays G.711 between two legs that negotiated the SAME payload type (the
	// bridge does not transcode). diago's default is PCMU-first, which would leave a
	// PCMA trunk bridged to a PCMU phone.
	r.dg = diago.NewDiago(ua,
		diago.WithTransport(transport),
		diago.WithServer(srv),
		diago.WithMediaConfig(diago.MediaConfig{
			Codecs: []media.Codec{CodecPCMA, CodecPCMU, CodecTelephoneEvent},
		}),
	)

	if err := r.ensureMediaPortRange(); err != nil {
		return err
	}

	if err := r.dg.ServeBackground(r.ctx, r.recoveredDialog(r.onInboundDialog)); err != nil {
		return fmt.Errorf("branch registrar: serve: %w", err)
	}
	go r.runReaper()

	// OPTIONS qualify: actively probe registered contacts so a phone that died without
	// de-registering (crash, network drop) is detected in ~1 min instead of waiting out
	// its registration TTL, and its NAT pinhole is kept fresh. Fail-safe: if the client
	// can't be built we simply skip qualify (never false-evict a reachable phone).
	if client, err := sipgo.NewClient(r.ua); err != nil {
		r.logger.Printf("[branch-registrar] OPTIONS qualify disabled (client init failed): %v", err)
	} else {
		r.client = client
		go r.runQualify()
	}

	r.logger.Printf("[branch-registrar] listening on %s:%d (realm=%q)", r.cfg.PublicHost, r.cfg.ListenPort, r.cfg.Realm)
	return nil
}

// SetRateLimiter injects the per-source-IP REGISTER throttle. Call before Start.
// Nil-safe (no limiter = unlimited).
func (r *BranchRegistrar) SetRateLimiter(l cache.RateLimiter) { r.limiter = l }

// ensureMediaPortRange guarantees diago has a valid RTP port window before serving.
// diago draws RTP ports from the package-global media.RTPPortStart/End; the trunk
// manager sets them, but if it did not run they stay 0 and diago binds OS ephemeral
// ports OUTSIDE the firewall RTP window, which blackholes branch audio with no error.
// So: if the globals are unset, set them from our config; if that is also invalid,
// refuse to start rather than blackhole silently. If they are already set (the normal
// case, by the trunk manager) we share that window deliberately.
func (r *BranchRegistrar) ensureMediaPortRange() error {
	if media.RTPPortStart != 0 && media.RTPPortEnd != 0 {
		r.logger.Printf("[branch-registrar] media RTP port window %d-%d (shared with trunk manager)", media.RTPPortStart, media.RTPPortEnd)
		return nil
	}
	s, e := r.cfg.RTPPortStart, r.cfg.RTPPortEnd
	if s <= 0 || e <= s {
		return fmt.Errorf("branch registrar: RTP port window is unset and config is invalid (start=%d end=%d); refusing to start to avoid binding OS ephemeral ports outside the firewall RTP window (silent no-audio)", s, e)
	}
	media.RTPPortStart = s
	media.RTPPortEnd = e
	r.logger.Printf("[branch-registrar] set media RTP port window %d-%d (trunk manager had not)", s, e)
	return nil
}

// Stop gracefully drains the registrar before tearing it down: it CANCELs in-flight
// rings (so a ringing phone stops) and sends BYE to every answered/bridged branch leg
// (so the phone hangs up cleanly instead of hearing dead air until its own timer
// fires), then cancels the serving context. Idempotent.
func (r *BranchRegistrar) Stop() {
	r.drainOnce.Do(r.drain)
	if r.cancel != nil {
		r.cancel()
	}
}

// drain runs the graceful-shutdown teardown once. Bounded so a wedged BYE cannot hang
// process shutdown.
func (r *BranchRegistrar) drain() {
	// CANCEL every in-flight ring so ringing phones stop.
	r.pendingMu.Lock()
	pendings := make([]*pendingRing, 0, len(r.pending))
	for _, pr := range r.pending {
		pendings = append(pendings, pr)
	}
	r.pendingMu.Unlock()
	for _, pr := range pendings {
		pr.peerCancelled.Store(true)
		pr.cancel() // diago sends SIP CANCEL
	}

	// BYE every answered/bridged phone leg, concurrently and time-bounded.
	r.dialogsMu.Lock()
	dialogs := make([]*diago.DialogClientSession, 0, len(r.dialogs))
	for _, d := range r.dialogs {
		dialogs = append(dialogs, d)
	}
	r.dialogsMu.Unlock()
	if len(dialogs) == 0 {
		return
	}
	r.logger.Printf("[branch-registrar] draining %d active branch leg(s) on shutdown", len(dialogs))
	var wg sync.WaitGroup
	for _, d := range dialogs {
		wg.Add(1)
		go func(d *diago.DialogClientSession) {
			defer wg.Done()
			defer r.recoverGoroutine("drain BYE")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = d.Hangup(ctx)
		}(d)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		r.logger.Printf("[branch-registrar] drain timed out; forcing teardown")
	}
}

// recovered wraps a SIP request handler so a panic — a latent bug, or a crafted packet
// that trips the sipgo/diago parser — is contained to that one transaction instead of
// crashing the whole process. The SIP port is public and pre-authentication, so an
// unrecovered panic here is a remote DoS on all of Vozko.
func (r *BranchRegistrar) recovered(name string, fn func(*sip.Request, sip.ServerTransaction)) func(*sip.Request, sip.ServerTransaction) {
	return func(req *sip.Request, tx sip.ServerTransaction) {
		defer func() {
			if rec := recover(); rec != nil {
				r.logger.Printf("[branch-registrar] recovered panic in %s handler: %v\n%s", name, rec, debug.Stack())
				func() {
					defer func() { _ = recover() }() // never let the 500 itself panic
					_ = tx.Respond(sip.NewResponseFromRequest(req, 500, "Server Error", nil))
				}()
			}
		}()
		fn(req, tx)
	}
}

// recoveredDialog is recovered for the diago inbound-dialog handler signature.
func (r *BranchRegistrar) recoveredDialog(fn func(*diago.DialogServerSession)) func(*diago.DialogServerSession) {
	return func(d *diago.DialogServerSession) {
		defer func() {
			if rec := recover(); rec != nil {
				r.logger.Printf("[branch-registrar] recovered panic in inbound dialog handler: %v\n%s", rec, debug.Stack())
			}
		}()
		fn(d)
	}
}

// recoverGoroutine contains a panic in a background goroutine so it degrades that one
// task instead of crashing the process. Use as a deferred call at the goroutine's top.
func (r *BranchRegistrar) recoverGoroutine(what string) {
	if rec := recover(); rec != nil {
		r.logger.Printf("[branch-registrar] recovered panic in %s: %v\n%s", what, rec, debug.Stack())
	}
}

// sourceHost is the IP (no port) a request arrived from, the rate-limit key.
func sourceHost(req *sip.Request) string {
	src := req.Source()
	if host, _, err := net.SplitHostPort(src); err == nil {
		return host
	}
	return src
}

// mediaProbeTarget is the address determineOutboundIP dials to discover which local
// interface holds the default route. It is a UDP "connect" only (no packets are sent),
// so it works offline as long as a default/LAN route exists; it never reaches Google.
const mediaProbeTarget = "8.8.8.8:53"

// resolveMediaBind returns (bindHost, advertiseHost) for the diago transport, pinning
// media to a real interface so RTP never binds to a link-local adapter:
//   - PublicHost is a concrete routable IP (a VPS public IP on the NIC, or the LAN IP
//     set for homologation) -> bind and advertise it directly.
//   - PublicHost is empty/loopback (e.g. same-box homologation with 127.0.0.1) -> detect
//     the outbound interface and use it for BOTH bind and advertise, so the SDP c= line
//     and the socket agree and the softphone's RTP actually reaches us.
//   - PublicHost is a DNS hostname (real deploy) -> bind to the outbound interface but
//     keep advertising the hostname.
//   - detection fails -> fall back to the previous 0.0.0.0 wildcard bind.
func (r *BranchRegistrar) resolveMediaBind() (bindHost, advertiseHost string) {
	host := strings.TrimSpace(r.cfg.PublicHost)
	ip := net.ParseIP(host)

	if ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
		return host, host // concrete routable IP: bind + advertise it
	}

	out, err := determineOutboundIP(mediaProbeTarget)
	if err != nil || out == nil {
		r.logger.Printf("[branch-registrar] could not detect outbound interface (%v); binding media to 0.0.0.0 (RTP may pick a wrong NIC)", err)
		if host == "" {
			host = "0.0.0.0"
		}
		return "0.0.0.0", host
	}

	if ip == nil && host != "" {
		// PublicHost is a hostname: bind to the real interface, advertise the hostname.
		return out.String(), host
	}
	// PublicHost is empty or loopback: use the detected interface for both.
	return out.String(), out.String()
}

// --- REGISTER ------------------------------------------------------------

func (r *BranchRegistrar) onRegister(req *sip.Request, tx sip.ServerTransaction) {
	// Anti-flood / anti-brute-force: cap REGISTER attempts per source IP before doing
	// ANY work (the DB lookup for an unknown user is the amplification vector). A spray
	// gets a cheap 503; a legitimate phone refreshing every ~60-120s never trips it.
	if r.limiter != nil {
		if allowed, _, _ := r.limiter.Allow(sourceHost(req)); !allowed {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 503, "Service Unavailable", nil))
			return
		}
	}

	rreq := branch.RegisterRequest{
		SIPUser:          aorUser(req),
		ReceivedFrom:     req.Source(),
		Method:           "REGISTER",
		CallID:           headerValue(req, "Call-ID"),
		UserAgent:        headerValue(req, "User-Agent"),
		RequestedExpires: parseExpires(req),
	}
	if c := req.Contact(); c != nil {
		rreq.Contact = c.Address.String()
		rreq.Transport = "udp"
	}
	if authH := req.GetHeader("Authorization"); authH != nil {
		if cred, err := digest.ParseCredentials(authH.Value()); err == nil {
			rreq.HasAuth = true
			rreq.SIPUser = cred.Username // the authenticated identity wins
			rreq.Realm = cred.Realm
			rreq.Nonce = cred.Nonce
			rreq.URI = cred.URI
			rreq.Response = cred.Response
			rreq.QOP = cred.QOP
			rreq.CNonce = cred.Cnonce
			if cred.Nc > 0 {
				rreq.NC = fmt.Sprintf("%08x", cred.Nc)
			}
		}
	}

	res, err := r.handle.Execute(rreq)
	if err != nil {
		r.logger.Printf("[branch-registrar] register error for %q: %v", rreq.SIPUser, err)
		_ = tx.Respond(sip.NewResponseFromRequest(req, 500, "Server Error", nil))
		return
	}
	// One line per attempt so a wrong username / bad password is visible instead of
	// silent (a challenge with no follow-up looks like nothing happened otherwise).
	r.logger.Printf("[branch-registrar] REGISTER from %s user=%q hasAuth=%v expires=%d -> %s",
		rreq.ReceivedFrom, rreq.SIPUser, rreq.HasAuth, rreq.RequestedExpires, res.Action)

	switch res.Action {
	case branch.RegisterChallenge:
		// stale=true tells the phone its nonce merely expired, so it retries WITHOUT
		// re-prompting the user for the password (RFC 7616 §3.3).
		r.challenge(req, tx, res.Realm, res.Nonce, res.Stale)
	case branch.RegisterUnauthorized:
		// Bad credentials (or an unknown/disabled extension, which is deliberately
		// indistinguishable): re-challenge with the fresh domain-issued nonce.
		r.challenge(req, tx, res.Realm, res.Nonce, false)
	case branch.RegisterOK:
		resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
		if c := req.Contact(); c != nil {
			resp.AppendHeader(c)
		}
		exp := sip.ExpiresHeader(res.GrantedExpires)
		resp.AppendHeader(&exp)
		_ = tx.Respond(resp)
	case branch.RegisterDeregistered:
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	case branch.RegisterIntervalTooBrief:
		resp := sip.NewResponseFromRequest(req, 423, "Interval Too Brief", nil)
		resp.AppendHeader(sip.NewHeader("Min-Expires", strconv.Itoa(res.MinExpires)))
		_ = tx.Respond(resp)
	case branch.RegisterForbidden:
		_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
	default: // RegisterNotFound
		_ = tx.Respond(sip.NewResponseFromRequest(req, 404, "Not Found", nil))
	}
}

func (r *BranchRegistrar) challenge(req *sip.Request, tx sip.ServerTransaction, realm, nonce string, stale bool) {
	resp := sip.NewResponseFromRequest(req, 401, "Unauthorized", nil)
	chal := digest.Challenge{Realm: realm, Nonce: nonce, Algorithm: "MD5", QOP: []string{"auth"}, Stale: stale}
	resp.AppendHeader(sip.NewHeader("WWW-Authenticate", chal.String()))
	_ = tx.Respond(resp)
}

func (r *BranchRegistrar) onOptions(req *sip.Request, tx sip.ServerTransaction) {
	_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
}

// onInboundDialog handles an INVITE that a phone sends TO Vozko (a branch
// originating a call). Branch-originated calling is a later phase; for now reject
// it cleanly so the dialog does not hang.
func (r *BranchRegistrar) onInboundDialog(inDialog *diago.DialogServerSession) {
	r.logger.Printf("[branch-registrar] inbound INVITE from a branch is not supported yet; rejecting")
	_ = inDialog.Respond(sip.StatusForbidden, "Branch-originated calls not enabled", nil)
}

// --- BranchRing: ring a registered phone ---------------------------------

func (r *BranchRegistrar) Ring(req branch.BranchRingRequest) error {
	routeRes, err := r.route.Execute(req.WorkspaceID, req.SIPUser)
	if err != nil {
		return err
	}
	if routeRes.Reason != branch.RouteOK || len(routeRes.Contacts) == 0 {
		// Not reachable: the transfer offer is rejected synchronously, so Initiate
		// rolls back the reservation and the caller simply stays with the agent.
		return fmt.Errorf("branch %s not ringable: %s", req.SIPUser, routeRes.Reason)
	}
	// Ring the first live contact. Forking to all contacts (first-200-wins) is a
	// refinement that reuses the offer-broker race; single contact keeps v1 simple.
	binding := routeRes.Contacts[0]
	recipient, err := recipientURI(binding)
	if err != nil {
		return err
	}
	go r.placeInvite(req, recipient)
	return nil
}

// CancelRing stops an in-flight ring: it marks the pending INVITE as peer-cancelled
// (so placeInvite tears down quietly, without declining the transfer back) and
// cancels its context, which makes diago send a SIP CANCEL and the phone stop
// ringing. A no-op if the branch is not currently ringing this call.
func (r *BranchRegistrar) CancelRing(req branch.BranchRingRequest) error {
	r.pendingMu.Lock()
	pr := r.pending[req.BranchID]
	r.pendingMu.Unlock()
	if pr == nil {
		return nil
	}
	r.logger.Printf("[branch-registrar] branch %s: cancelling ring for call %s (answered/declined elsewhere)", req.SIPUser, req.CallID)
	pr.peerCancelled.Store(true)
	pr.cancel()
	return nil
}

func (r *BranchRegistrar) placeInvite(req branch.BranchRingRequest, recipient sip.Uri) {
	defer r.recoverGoroutine("placeInvite branch=" + req.BranchID)
	// No-answer ring timer, mirroring Asterisk Dial()'s wait_for_answer timeout: the
	// INVITE is bounded by a fixed ring time, so a phone that rings without answering
	// is cancelled and the caller is returned to the transferring agent. Cancelling
	// the ctx makes diago send CANCEL, stopping the ring.
	ringSeconds := defaultBranchRingSeconds
	ctx, cancel := context.WithTimeout(r.ctx, time.Duration(ringSeconds)*time.Second)
	defer cancel()

	// Track this ring so CancelRing can stop it the instant the offer is answered on
	// another contact or declined.
	pr := &pendingRing{cancel: cancel}
	r.pendingMu.Lock()
	r.pending[req.BranchID] = pr
	r.pendingMu.Unlock()
	defer func() {
		r.pendingMu.Lock()
		if r.pending[req.BranchID] == pr {
			delete(r.pending, req.BranchID)
		}
		r.pendingMu.Unlock()
	}()

	r.logger.Printf("[branch-registrar] ringing branch %s (call=%s) at %s:%d for up to %ds",
		req.SIPUser, req.CallID, recipient.Host, recipient.Port, ringSeconds)

	dialog, err := r.dg.Invite(ctx, recipient, diago.InviteOptions{})
	if err != nil {
		// Answered/declined on another contact: the transfer is already resolved
		// elsewhere, so just stop ringing without declining the transfer back.
		if pr.peerCancelled.Load() {
			r.logger.Printf("[branch-registrar] branch %s ring for call %s stopped (answered/declined elsewhere)", req.SIPUser, req.CallID)
			return
		}
		r.logger.Printf("[branch-registrar] INVITE to branch %s failed: %v", req.SIPUser, err)
		r.declineBack(req)
		return
	}
	dialogID := string(dialog.ID)
	r.dialogsMu.Lock()
	r.dialogs[dialogID] = dialog
	r.dialogsMu.Unlock()

	// The phone answered (200 OK). Apply symmetric-RTP latching to the branch leg
	// exactly like a trunk leg, then wrap it as a voip.MediaSession for the bridge.
	ms := dialog.MediaSession()
	if ms == nil {
		r.logger.Printf("[branch-registrar] branch %s answered without a media session; hanging up", req.SIPUser)
		_ = dialog.Hangup(context.Background())
		return
	}
	if err := WrapMediaSessionForLatching(ms, LatchOptions{
		ExpectedPTs: PayloadTypesFromCodecs(ms.Codecs),
		TrunkID:     "branch:" + req.BranchID,
		CallID:      dialog.ID,
		Logger:      r.logger,
	}); err != nil {
		r.logger.Printf("[branch-registrar] branch %s RTP latching unavailable: %v", req.SIPUser, err)
	}

	phoneMedia := &mediaSessionAdapter{session: ms, diagLog: r.logger, diagID: dialogID}
	hangupPhone := func() {
		_ = dialog.Hangup(context.Background())
		r.dialogsMu.Lock()
		delete(r.dialogs, dialogID)
		r.dialogsMu.Unlock()
	}

	// Park the answered leg for the synchronous StartBridge that Accept triggers,
	// then complete the transfer through the REUSED engine: Accept -> executor
	// SwapBlind -> branchSession.AttachLeg -> StartBridge (consumes this leg) -> RTP
	// relay. No new transfer logic, exactly as plan §5.4 prescribes.
	r.answeredMu.Lock()
	r.answered[req.BranchID] = &answeredBranch{media: phoneMedia, hangup: hangupPhone}
	r.answeredMu.Unlock()

	if r.transferUC == nil {
		r.logger.Printf("[branch-registrar] branch %s answered but the transfer engine is not wired; hanging up", req.SIPUser)
		if a := r.clearAnswered(req.BranchID); a != nil {
			a.hangup()
		}
		return
	}

	// The branch answered: its branch session id ("branch:"+branchID) is the winner, so
	// the swap lands on the phone and the member's browser contact is cancelled.
	if _, err := r.transferUC.Accept(context.Background(), req.TransferID, req.UserID, "branch:"+req.BranchID); err != nil {
		r.logger.Printf("[branch-registrar] branch %s: transfer Accept failed for call %s: %v", req.SIPUser, req.CallID, err)
		if a := r.clearAnswered(req.BranchID); a != nil {
			a.hangup() // StartBridge never consumed it
		}
		return
	}
	r.logger.Printf("[branch-registrar] branch %s answered call %s (dialog %s) and is now bridged to the caller", req.SIPUser, req.CallID, dialogID)
}

// StartBridge implements branch.BranchMediaBridge: it relays the surrendered caller
// media to the branch's just-answered phone leg (parked by placeInvite). It is called
// synchronously from branchSession.AttachLeg inside the transfer executor's swap.
func (r *BranchRegistrar) StartBridge(branchID, callID string, caller voip.MediaSession, onEnd func()) (func(), error) {
	a := r.clearAnswered(branchID)
	if a == nil {
		return nil, fmt.Errorf("branch %s: no answered dialog to bridge call %s", branchID, callID)
	}
	if caller == nil {
		a.hangup()
		return nil, fmt.Errorf("branch %s: nil caller media for call %s", branchID, callID)
	}

	bridge := NewRTPBridge(a.media, caller, r.logger)
	bridge.Start(r.ctx)
	r.logger.Printf("[branch-registrar] branch %s: RTP bridge up for call %s", branchID, callID)

	// When the bridge ends (either leg hangs up), BYE the phone and let the session
	// tear the caller leg down (onEnd). Done fires exactly once.
	go func() {
		<-bridge.Done()
		r.logger.Printf("[branch-registrar] branch %s: RTP bridge for call %s ended", branchID, callID)
		a.hangup()
		if onEnd != nil {
			onEnd()
		}
	}()

	return bridge.Stop, nil
}

func (r *BranchRegistrar) clearAnswered(branchID string) *answeredBranch {
	r.answeredMu.Lock()
	a := r.answered[branchID]
	delete(r.answered, branchID)
	r.answeredMu.Unlock()
	return a
}

// declineBack returns the caller to the transferring agent when a rung branch
// could not take the call (busy / no-answer / decline). The caller never left the
// original agent's leg, so Decline just releases the ring reservation and the
// caller stays with the agent.
func (r *BranchRegistrar) declineBack(req branch.BranchRingRequest) {
	if r.transferUC != nil && req.TransferID != "" {
		if err := r.transferUC.Decline(context.Background(), req.TransferID, req.UserID, "no_answer"); err != nil {
			r.logger.Printf("[branch-registrar] branch %s: transfer Decline failed: %v", req.SIPUser, err)
		}
	}
}

// --- expiry reaper -------------------------------------------------------

func (r *BranchRegistrar) runReaper() {
	t := time.NewTicker(r.cfg.ReapInterval)
	defer t.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-t.C:
			r.reapOnce() // recovers internally, so one bad tick never kills the reaper
		}
	}
}

func (r *BranchRegistrar) reapOnce() {
	defer r.recoverGoroutine("reaper tick")
	for _, b := range r.store.ReapExpired(time.Now()) {
		r.takeOfflineIfLast(b.SIPUser, b.BranchID)
	}
}

// takeOfflineIfLast marks a branch unreachable (routing target + dashboard) once the
// just-removed contact was its last live one. Shared by the TTL reaper and OPTIONS
// qualify so both drive the same presence-offline transition.
func (r *BranchRegistrar) takeOfflineIfLast(sipUser, branchID string) {
	if branchID == "" {
		return
	}
	if n, _ := r.store.CountLive(sipUser); n == 0 {
		if r.presence != nil {
			r.presence.OnBranchUnreachable(branchID)
		}
		if r.status != nil {
			_ = r.status.UpdateRegistrationStatus(branchID, branch.RegistrationStatusUnreachable)
		}
	}
}

// --- OPTIONS qualify (active liveness) -----------------------------------

const (
	qualifyInterval  = 30 * time.Second // probe every registered contact this often
	qualifyTimeout   = 3 * time.Second  // per-probe wait for any SIP response
	qualifyMaxMisses = 2                // consecutive no-response probes before eviction
)

// bindingLister is the subset of a durable store that can enumerate every live contact,
// which qualify needs. The Redis store implements it; the in-memory store does not, so
// qualify is a no-op there (dev/CI).
type bindingLister interface {
	ListAllLive() ([]branch.RegistrationBinding, error)
}

func (r *BranchRegistrar) runQualify() {
	defer r.recoverGoroutine("qualify loop")
	lister, ok := r.store.(bindingLister)
	if !ok {
		return // store can't enumerate contacts (in-memory); qualify unavailable
	}
	t := time.NewTicker(qualifyInterval)
	defer t.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-t.C:
			r.qualifyOnce(lister)
		}
	}
}

func (r *BranchRegistrar) qualifyOnce(lister bindingLister) {
	defer r.recoverGoroutine("qualify tick")
	bindings, err := lister.ListAllLive()
	if err != nil {
		return
	}
	for _, b := range bindings {
		go r.qualifyContact(b)
	}
}

// qualifyContact sends one OPTIONS to a contact's real address (rewrite_contact) and
// evicts it after qualifyMaxMisses consecutive no-response probes. ANY SIP response
// (even 405/486) means the phone is alive; only a timeout counts as a miss.
func (r *BranchRegistrar) qualifyContact(b branch.RegistrationBinding) {
	defer r.recoverGoroutine("qualify " + b.SIPUser)
	uri, err := recipientURI(b)
	if err != nil {
		return
	}
	key := b.SIPUser + "|" + b.CallID
	if r.probe(uri) {
		r.qualifyMu.Lock()
		delete(r.qualifyMisses, key)
		r.qualifyMu.Unlock()
		return
	}
	r.qualifyMu.Lock()
	r.qualifyMisses[key]++
	misses := r.qualifyMisses[key]
	r.qualifyMu.Unlock()
	if misses < qualifyMaxMisses {
		return
	}
	r.logger.Printf("[branch-registrar] qualify: branch %s contact %s missed %d OPTIONS; evicting", b.SIPUser, b.CallID, misses)
	_ = r.store.Remove(b.SIPUser, b.CallID)
	r.qualifyMu.Lock()
	delete(r.qualifyMisses, key)
	r.qualifyMu.Unlock()
	r.takeOfflineIfLast(b.SIPUser, b.BranchID)
}

// probeOPTIONS returns true if the contact answered ANY SIP response, false only on a
// timeout / transport error. Fail-safe: with no client, report reachable (never evict).
func (r *BranchRegistrar) probeOPTIONS(recipient sip.Uri) bool {
	if r.client == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(r.ctx, qualifyTimeout)
	defer cancel()
	res, err := r.client.Do(ctx, sip.NewRequest(sip.OPTIONS, recipient))
	return err == nil && res != nil
}

// --- parsing helpers -----------------------------------------------------

func aorUser(req *sip.Request) string {
	if to := req.To(); to != nil && to.Address.User != "" {
		return to.Address.User
	}
	if from := req.From(); from != nil {
		return from.Address.User
	}
	return ""
}

func headerValue(req *sip.Request, name string) string {
	if h := req.GetHeader(name); h != nil {
		return h.Value()
	}
	return ""
}

// parseExpires reads the requested registration expiry from the Expires header or
// the Contact expires param; -1 means "unset" so the domain uses its default.
func parseExpires(req *sip.Request) int {
	if h := req.GetHeader("Expires"); h != nil {
		if v, err := strconv.Atoi(strings.TrimSpace(h.Value())); err == nil {
			return v
		}
	}
	if c := req.Contact(); c != nil {
		if v, ok := c.Params.Get("expires"); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return n
			}
		}
	}
	return -1
}

// recipientURI builds the INVITE target from a binding, applying rewrite_contact:
// the request is routed to the real packet source (received_from), NOT the
// phone's advertised (often private) Contact host.
func recipientURI(b branch.RegistrationBinding) (sip.Uri, error) {
	var uri sip.Uri
	if err := sip.ParseUri(b.Contact, &uri); err != nil {
		// Fall back to a bare user@host built from received_from.
		uri = sip.Uri{User: b.SIPUser}
	}
	if host, portStr, err := net.SplitHostPort(strings.TrimSpace(b.ReceivedFrom)); err == nil {
		uri.Host = host
		if p, err := strconv.Atoi(portStr); err == nil {
			uri.Port = p
		}
	}
	if uri.Host == "" {
		return sip.Uri{}, fmt.Errorf("branch %s: no reachable address (received_from=%q)", b.SIPUser, b.ReceivedFrom)
	}
	return uri, nil
}
