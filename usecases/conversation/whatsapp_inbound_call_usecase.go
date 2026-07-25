package conversation_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	conversation_domain "vozko/domain/conversation"
	dialer "vozko/domain/dialer"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wce "vozko/domain/whatsapp_campaign_entry"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	wsc "vozko/domain/workspace_config"
	"vozko/infra/conversation/whatsapp/media"
	dialer_usecase "vozko/usecases/dialer"
)

const (
	whatsappInboundEntryType    = "whatsapp"
	whatsappInboundPerAgentRing = 15 * time.Second

	whatsappInboundTotalWindow = 28 * time.Second
)

type inboundBusinessPhoneResolver interface {
	FindByMetaPhoneNumberID(metaPhoneNumberID string) (*businessphone.WhatsAppBusinessPhoneNumber, error)
}

type inboundEntryResolver interface {
	FindByNumberAndBusinessPhone(number, businessPhoneID string) (*wce.WhatsAppCampaignEntry, error)
}

type inboundAssignmentReader interface {
	GetAssignedUserID(workspaceID, entryID, entryType string) string
}

type inboundDepartmentResolver interface {
	GetEntryDepartmentID(entryID, entryType string) (string, error)
}

type inboundEligibleUsers interface {
	GetEligibleUsersForWorkspace(workspaceID string, skipAdmins bool) []string
	GetEligibleUsersForWorkspaceDepartment(workspaceID, departmentID string, skipAdmins bool) []string
}

type inboundAssignmentWriter interface {
	Reassign(entryID, entryType, businessPhoneID, workspaceID, userID string) error
}

type inboundUserResolver interface {
	ResolveUsernames(userIDs []string) map[string]string
}

// inboundWorkspaceConfig reads the workspace's SkipAdminAssignment setting so
// roulette routing can exclude owners/admins when the workspace disabled it,
// matching inbox and SIP call roulette behaviour. Optional.
type inboundWorkspaceConfig interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type WhatsAppInboundCallUseCase struct {
	signaling   conversation_domain.WhatsAppCallSignaling
	registry    conversation_domain.WhatsAppCallRegistry
	phones      inboundBusinessPhoneResolver
	entries     inboundEntryResolver
	assignment  inboundAssignmentReader
	assigner    inboundAssignmentWriter
	departments inboundDepartmentResolver
	eligible    inboundEligibleUsers
	sessions    dialer.DialerSessionRegistry
	admission   dialer.CallAdmissionCoordinator
	broker      *dialer_usecase.InboundOfferBroker
	executor    dialer.InboundCRMCallExecutor
	messages    conversation_domain.MessageRepository
	hub         conversation_domain.EventBroadcaster
	users       inboundUserResolver
	wsConfig    inboundWorkspaceConfig

	publicIP    string
	stunServers []string
	log         *log.Logger

	mu       sync.Mutex
	inFlight map[string]bool
}

type WhatsAppInboundConfig struct {
	Signaling   conversation_domain.WhatsAppCallSignaling
	Registry    conversation_domain.WhatsAppCallRegistry
	Phones      inboundBusinessPhoneResolver
	Entries     inboundEntryResolver
	Assignment  inboundAssignmentReader
	Assigner    inboundAssignmentWriter
	Departments inboundDepartmentResolver
	Eligible    inboundEligibleUsers
	Sessions    dialer.DialerSessionRegistry
	Admission   dialer.CallAdmissionCoordinator
	Broker      *dialer_usecase.InboundOfferBroker
	Executor    dialer.InboundCRMCallExecutor
	// Messages + Hub record the call lifecycle (received/answered/missed/ended)
	// into the conversation thread and push it live. Optional.
	Messages conversation_domain.MessageRepository
	Hub      conversation_domain.EventBroadcaster
	Users    inboundUserResolver
	// WorkspaceConfig is optional; when set, roulette routing honours the
	// workspace SkipAdminAssignment flag (admins excluded when enabled).
	WorkspaceConfig inboundWorkspaceConfig
	PublicIP        string
	StunServers     []string
	Logger          *log.Logger
}

func NewWhatsAppInboundCallUseCase(cfg WhatsAppInboundConfig) *WhatsAppInboundCallUseCase {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &WhatsAppInboundCallUseCase{
		signaling:   cfg.Signaling,
		registry:    cfg.Registry,
		phones:      cfg.Phones,
		entries:     cfg.Entries,
		assignment:  cfg.Assignment,
		assigner:    cfg.Assigner,
		departments: cfg.Departments,
		eligible:    cfg.Eligible,
		sessions:    cfg.Sessions,
		admission:   cfg.Admission,
		broker:      cfg.Broker,
		executor:    cfg.Executor,
		messages:    cfg.Messages,
		hub:         cfg.Hub,
		users:       cfg.Users,
		wsConfig:    cfg.WorkspaceConfig,
		publicIP:    strings.TrimSpace(cfg.PublicIP),
		stunServers: cfg.StunServers,
		log:         logger,
		inFlight:    make(map[string]bool),
	}
}

func (uc *WhatsAppInboundCallUseCase) HandleInboundConnect(c conversation_domain.WhatsAppInboundConnect) {
	go uc.handle(c)
}

func (uc *WhatsAppInboundCallUseCase) handle(c conversation_domain.WhatsAppInboundConnect) {
	if !uc.claim(c.CallID) {
		return
	}
	defer uc.unclaim(c.CallID)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	phone, err := uc.phones.FindByMetaPhoneNumberID(c.MetaPhoneNumberID)
	if err != nil || phone == nil {
		uc.log.Printf("[WAInbound] %s: unknown business phone %s: %v", c.CallID, c.MetaPhoneNumberID, err)
		return
	}
	businessPhoneID := phone.ID
	workspaceID := strings.TrimSpace(phone.OwnerWorkspaceID)

	entryID, departmentID, assignedUserID := "", "", ""
	if entry, eerr := uc.entries.FindByNumberAndBusinessPhone(c.FromNumber, businessPhoneID); eerr == nil && entry != nil {
		entryID = entry.ID
		if dep, derr := uc.departments.GetEntryDepartmentID(entryID, whatsappInboundEntryType); derr == nil {
			departmentID = dep
		}
	}
	if workspaceID == "" {
		uc.log.Printf("[WAInbound] %s: no workspace for business phone %s → reject", c.CallID, businessPhoneID)
		uc.reject(ctx, businessPhoneID, c.CallID)
		return
	}
	if entryID != "" {
		assignedUserID = uc.assignment.GetAssignedUserID(workspaceID, entryID, whatsappInboundEntryType)
	}

	uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallReceived, "📞 Chamada recebida pelo WhatsApp.")

	candidates, roulette := uc.resolveCandidates(workspaceID, departmentID, assignedUserID)
	if len(candidates) == 0 {
		uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallMissed, "📞 Chamada perdida. Nenhum agente disponível.")
		uc.reject(ctx, businessPhoneID, c.CallID)
		return
	}

	lease, err := uc.admission.Acquire(ctx, dialer.CallAdmissionInput{
		WorkspaceID:      workspaceID,
		SlotPollInterval: time.Second,
		SlotPollTimeout:  5 * time.Second,
		ReservationTTL:   5 * time.Minute,
		CallChannel:      workspace_pricing.TelephonyChannelWhatsApp,
	})
	if err != nil {
		uc.log.Printf("[WAInbound] %s: admission failed: %v → reject", c.CallID, err)
		uc.reject(ctx, businessPhoneID, c.CallID)
		return
	}
	leaseReleased := false
	defer func() {
		if !leaseReleased {
			_ = uc.admission.Release(lease)
		}
	}()

	sess, err := media.NewSessionAnswerer(c.SDPOffer, uc.publicIP, uc.stunServers)
	if err != nil {
		uc.log.Printf("[WAInbound] %s: media answerer failed: %v → reject", c.CallID, err)
		uc.reject(ctx, businessPhoneID, c.CallID)
		return
	}
	closeMedia := true
	defer func() {
		if closeMedia {
			_ = sess.Close()
		}
	}()
	if err := uc.signaling.PreAcceptCall(ctx, businessPhoneID, c.CallID, sess.Answer()); err != nil {
		uc.log.Printf("[WAInbound] %s: pre_accept failed: %v → reject", c.CallID, err)
		uc.reject(ctx, businessPhoneID, c.CallID)
		return
	}

	signals := uc.registry.Register(c.CallID)
	defer uc.registry.Unregister(c.CallID)

	deadline := time.Now().Add(whatsappInboundTotalWindow)
	outcome := uc.ringSequentially(ctx, c, businessPhoneID, workspaceID, entryID, roulette, candidates, signals, deadline)
	if outcome.terminated {
		uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallMissed, "📞 Chamada perdida.")
		return
	}
	if outcome.session == nil {
		if outcome.declinedByUserID != "" {
			uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallMissed,
				"📞 Chamada recusada por "+uc.usernameOf(outcome.declinedByUserID)+".")
		} else {
			uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallMissed, "📞 Chamada não atendida.")
		}
		uc.reject(ctx, businessPhoneID, c.CallID)
		return
	}

	// The agent accepted with its ring reservation still held (ringCandidate keeps
	// it on accept). Hold it across the blocking AcceptCall round-trip and the
	// attach below so no concurrent flow can grab the agent in that window; release
	// on any failure in between. AttachInboundCRMCall consumes the reservation on
	// success (reserved->active), after which the deferred release is a no-op.
	reservationActive := true
	defer func() {
		if reservationActive {
			outcome.session.Release(outcome.offerID)
		}
	}()

	if err := uc.signaling.AcceptCall(ctx, businessPhoneID, c.CallID, sess.Answer(), c.CallID); err != nil {
		uc.log.Printf("[WAInbound] %s: accept failed: %v → reject", c.CallID, err)
		uc.reject(ctx, businessPhoneID, c.CallID)
		return
	}
	uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallAnswered, "📞 Chamada atendida.")
	answeredAt := time.Now()

	call := newWhatsAppInboundCall("wa-in-"+c.CallID, businessPhoneID, c.CallID, c.FromNumber, sess, uc.signaling, uc.log)
	go func() {
		for sig := range signals {
			if sig.Kind == conversation_domain.WhatsAppCallTerminate {
				call.markEnded(sig.Reason)
				return
			}
		}
	}()

	if err := uc.executor.AttachInboundCRMCall(ctx, dialer.AttachInboundCRMCallInput{
		OfferID:     outcome.offerID,
		WorkspaceID: workspaceID,
		UserID:      outcome.session.UserID(),
		Session:     outcome.session,
		PhoneNumber: c.FromNumber,
		Call:        call,
		Admission:   lease,
		StartedAt:   time.Now(),
	}); err != nil {
		uc.log.Printf("[WAInbound] %s: attach failed: %v", c.CallID, err)
		_ = call.Hangup()
		return
	}
	// Attach consumed the reservation (reserved->active); disarm the deferred
	// release so the now-attached call is left untouched.
	reservationActive = false
	leaseReleased = true
	closeMedia = false

	if roulette && entryID != "" {
		uc.assignTo(businessPhoneID, workspaceID, entryID, outcome.session.UserID())
	}

	select {
	case <-ctx.Done():
		_ = call.Hangup()
	case <-call.Done():
	}
	uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallEnded,
		fmt.Sprintf("📞 Chamada encerrada. Duração %s.", formatCallDuration(time.Since(answeredAt))))
}

// usernameOf resolves a user id to a display name, falling back to a neutral
// label so a missed-call message never leaks an internal UUID.
func (uc *WhatsAppInboundCallUseCase) usernameOf(userID string) string {
	if uc.users != nil {
		if name := strings.TrimSpace(uc.users.ResolveUsernames([]string{userID})[userID]); name != "" {
			return name
		}
	}
	return "um agente"
}

type ringOutcome struct {
	session          dialer.DialerSession
	offerID          string
	terminated       bool
	declinedByUserID string // last agent who explicitly declined (none accepted)
}

func (uc *WhatsAppInboundCallUseCase) resolveCandidates(workspaceID, departmentID, assignedUserID string) ([]dialer.DialerSession, bool) {
	if strings.TrimSpace(assignedUserID) != "" {
		s, ok := uc.sessions.FindByUser(workspaceID, assignedUserID)
		online := ok && s != nil
		busy := online && s.HasActiveCall()
		if online && !busy {
			uc.log.Printf("[WAInbound] resolveCandidates: assigned=%s online=true hasActiveCall=false → 1 candidate (owner-only)", assignedUserID)
			return []dialer.DialerSession{s}, false
		}
		uc.log.Printf("[WAInbound] resolveCandidates: assigned=%s online=%t hasActiveCall=%t → 0 candidates (owner-only, no fallback)", assignedUserID, online, busy)
		return nil, false
	}

	// Honour the workspace SkipAdminAssignment flag so unassigned inbound calls
	// don't ring owners/admins when the workspace opted out, matching inbox and
	// SIP call roulette. Defaults to including admins when unset or on error.
	skipAdmins := false
	if uc.wsConfig != nil {
		if cfg, err := uc.wsConfig.GetByWorkspaceID(context.Background(), workspaceID); err == nil && cfg != nil {
			skipAdmins = cfg.SkipAdminAssignment
		}
	}

	var eligible []string
	if strings.TrimSpace(departmentID) != "" {
		eligible = uc.eligible.GetEligibleUsersForWorkspaceDepartment(workspaceID, departmentID, skipAdmins)
	} else {
		eligible = uc.eligible.GetEligibleUsersForWorkspace(workspaceID, skipAdmins)
	}
	allowed := make(map[string]bool, len(eligible))
	for _, u := range eligible {
		allowed[u] = true
	}

	available := uc.sessions.ListAvailable(workspaceID)
	dialerUserIDs := make([]string, 0, len(available))
	var candidates []dialer.DialerSession
	for _, s := range available {
		if s == nil || s.HasActiveCall() {
			continue
		}
		dialerUserIDs = append(dialerUserIDs, s.UserID())
		if allowed[s.UserID()] {
			candidates = append(candidates, s)
		}
	}
	// Diagnostic for "no agent available": when candidates is 0, the field that
	// is empty tells you the cause —
	//   eligible=[]         → a roulette-permission / SkipAdminAssignment / inbox
	//                         (/ws/conversations) presence problem (no one qualifies);
	//   dialerAvailable=[]  → nobody is connected to the dialer (/ws/dialer);
	//   both non-empty, 0   → the online dialer user isn't in the eligible (roulette) set.
	uc.log.Printf("[WAInbound] resolveCandidates: department=%q skipAdmins=%t eligible=%v dialerAvailable=%v → %d candidate(s)",
		departmentID, skipAdmins, eligible, dialerUserIDs, len(candidates))
	return candidates, true
}

func (uc *WhatsAppInboundCallUseCase) ringSequentially(
	ctx context.Context,
	c conversation_domain.WhatsAppInboundConnect,
	businessPhoneID, workspaceID, entryID string,
	roulette bool,
	candidates []dialer.DialerSession,
	signals <-chan conversation_domain.WhatsAppCallSignal,
	deadline time.Time,
) *ringOutcome {
	var lastDeclinedBy string
	for _, cand := range candidates {
		if time.Now().After(deadline) {
			break
		}
		if cand == nil || cand.HasActiveCall() {
			continue
		}
		ring := whatsappInboundPerAgentRing
		if rem := time.Until(deadline); rem < ring {
			ring = rem
		}
		if ring <= 0 {
			break
		}

		offerID := uuid.NewString()
		offer := dialer.InboundCallOffer{
			OfferID:     offerID,
			CallID:      c.CallID,
			WorkspaceID: workspaceID,
			FromNumber:  c.FromNumber,
			ToNumber:    c.ToNumber,
			Channel:     "whatsapp",
			ExpiresAt:   time.Now().Add(ring),
		}

		onReserved := func() {
			if roulette && entryID != "" {
				uc.assignTo(businessPhoneID, workspaceID, entryID, cand.UserID())
			}
		}

		switch uc.ringCandidate(ctx, cand, offer, signals, ring, onReserved) {
		case candAccepted:
			// The candidate accepted with its ring reservation still held; handle()
			// consumes it via Attach on success or releases it on a post-accept
			// failure. Do not release here.
			return &ringOutcome{session: cand, offerID: offerID}
		case candTerminated:
			return &ringOutcome{terminated: true}
		case candDeclined:
			lastDeclinedBy = cand.UserID()
		case candTimedOut, candUnavailable:
			// Ring window elapsed or the agent was grabbed concurrently / unreachable;
			// the reservation (if any) was already released — move to the next agent.
		}
	}
	return &ringOutcome{declinedByUserID: lastDeclinedBy}
}

// candidateOutcome is the result of ringing a single agent.
type candidateOutcome int

const (
	// candUnavailable: the agent could not be reserved (busy/grabbed concurrently)
	// or the ring failed to deliver. Try the next candidate.
	candUnavailable candidateOutcome = iota
	candAccepted
	candDeclined
	candTimedOut
	// candTerminated: the caller hung up or the context was cancelled mid-ring.
	candTerminated
)

// ringCandidate reserves one agent for the offer, rings them, and waits up to
// `ring` for an accept/decline. The reservation excludes the agent from every
// other routing flow's availability pool while their phone is ringing. A deferred
// release runs on every outcome EXCEPT accept — so decline, timeout, caller
// hangup, a failed notify, and even a panic all free the agent immediately,
// while an accepted agent stays reserved until handle() attaches the call (which
// consumes the reservation atomically). onReserved runs once the reservation is
// secured and before the ring is sent (used to record the roulette assignment).
func (uc *WhatsAppInboundCallUseCase) ringCandidate(
	ctx context.Context,
	cand dialer.DialerSession,
	offer dialer.InboundCallOffer,
	signals <-chan conversation_domain.WhatsAppCallSignal,
	ring time.Duration,
	onReserved func(),
) candidateOutcome {
	if !cand.Reserve(offer.OfferID) {
		return candUnavailable
	}
	accepted := false
	defer func() {
		if !accepted {
			cand.Release(offer.OfferID)
		}
	}()

	state := &dialer_usecase.InboundOfferState{
		Offer:           offer,
		TargetUserID:    cand.UserID(),
		TargetSessionID: cand.ID(),
		Response:        make(chan dialer_usecase.InboundOfferResponse, 1),
	}
	remove := uc.broker.Store(state)
	defer remove()

	if onReserved != nil {
		onReserved()
	}

	if err := cand.Notify(dialer.DialerControlMessage{Type: dialer.DialerControlInboundCall, Payload: offer}); err != nil {
		return candUnavailable
	}

	timer := time.NewTimer(ring)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return candTerminated
		case sig := <-signals:
			if sig.Kind == conversation_domain.WhatsAppCallTerminate {
				return candTerminated
			}
		case resp := <-state.Response:
			if resp.Accepted {
				accepted = true
				return candAccepted
			}
			return candDeclined
		case <-timer.C:
			return candTimedOut
		}
	}
}

func (uc *WhatsAppInboundCallUseCase) reject(ctx context.Context, businessPhoneID, callID string) {
	if strings.TrimSpace(businessPhoneID) == "" {
		return
	}
	if err := uc.signaling.RejectCall(ctx, businessPhoneID, callID); err != nil {
		uc.log.Printf("[WAInbound] %s: reject failed: %v", callID, err)
	}
}

func (uc *WhatsAppInboundCallUseCase) assignTo(businessPhoneID, workspaceID, entryID, userID string) {
	if uc.assigner == nil || strings.TrimSpace(entryID) == "" || strings.TrimSpace(userID) == "" {
		return
	}
	if err := uc.assigner.Reassign(entryID, whatsappInboundEntryType, businessPhoneID, workspaceID, userID); err != nil {
		uc.log.Printf("[WAInbound] reassign entry %s to user %s failed: %v", entryID, userID, err)
	}
}

func (uc *WhatsAppInboundCallUseCase) claim(callID string) bool {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	if uc.inFlight[callID] {
		return false
	}
	uc.inFlight[callID] = true
	return true
}

func (uc *WhatsAppInboundCallUseCase) unclaim(callID string) {
	uc.mu.Lock()
	delete(uc.inFlight, callID)
	uc.mu.Unlock()
}

func (uc *WhatsAppInboundCallUseCase) recordEvent(entryID, from string, msgType conversation_domain.MessageType, text string) {
	if uc.messages == nil || strings.TrimSpace(entryID) == "" {
		return
	}
	now := time.Now().UTC()
	m := &conversation_domain.Message{
		ID:          uuid.NewString(),
		EntryID:     entryID,
		EntryType:   shared.EntryTypeWhatsApp,
		Channel:     conversation_domain.MessageChannelWhatsApp,
		MessageType: msgType,
		From:        from,
		Text:        text,
		Read:        false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.Normalize()
	if err := uc.messages.Create(m); err != nil {
		uc.log.Printf("[WAInbound] record call event failed for entry %s: %v", entryID, err)
		return
	}
	if uc.hub != nil {
		uc.hub.BroadcastNewMessage(entryID, string(shared.EntryTypeWhatsApp), m)
	}
}

func formatCallDuration(d time.Duration) string {
	total := int(d.Seconds())
	if total < 0 {
		total = 0
	}
	if total < 60 {
		return fmt.Sprintf("%ds", total)
	}
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

var _ conversation_domain.WhatsAppInboundCallHandler = (*WhatsAppInboundCallUseCase)(nil)
