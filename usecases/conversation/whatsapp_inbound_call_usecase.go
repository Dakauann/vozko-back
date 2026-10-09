package conversation_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	callsession "vozko/domain/callsession"
	conversation_domain "vozko/domain/conversation"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wce "vozko/domain/whatsapp_campaign_entry"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	wsc "vozko/domain/workspace_config"
	"vozko/infra/conversation/whatsapp/media"
	callsession_usecase "vozko/usecases/callsession"
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
	FindInboundRouteByNumberAndBusinessPhone(number, businessPhoneID string) (*wce.WhatsAppCampaignEntry, error)
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
	sessions    callsession.CallSessionRegistry
	admission   callsession.CallAdmissionCoordinator
	ringer      *callsession_usecase.InboundRinger
	executor    callsession.InboundCRMCallExecutor
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
	Signaling       conversation_domain.WhatsAppCallSignaling
	Registry        conversation_domain.WhatsAppCallRegistry
	Phones          inboundBusinessPhoneResolver
	Entries         inboundEntryResolver
	Assignment      inboundAssignmentReader
	Assigner        inboundAssignmentWriter
	Departments     inboundDepartmentResolver
	Eligible        inboundEligibleUsers
	Sessions        callsession.CallSessionRegistry
	Admission       callsession.CallAdmissionCoordinator
	Broker          *callsession_usecase.InboundOfferBroker
	Executor        callsession.InboundCRMCallExecutor
	Messages        conversation_domain.MessageRepository
	Hub             conversation_domain.EventBroadcaster
	Users           inboundUserResolver
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
		ringer:      callsession_usecase.NewInboundRinger(cfg.Broker),
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
	if entry, eerr := uc.entries.FindInboundRouteByNumberAndBusinessPhone(c.FromNumber, businessPhoneID); eerr == nil && entry != nil {
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

	lease, err := uc.admission.Acquire(ctx, callsession.CallAdmissionInput{
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

	termination := watchTermination(uc.registry.Register(c.CallID))
	defer uc.registry.Unregister(c.CallID)

	outcome := uc.ringer.Ring(ctx, callsession_usecase.RingRequest{
		Offer: callsession.InboundCallOffer{
			CallID:      c.CallID,
			WorkspaceID: workspaceID,
			FromNumber:  c.FromNumber,
			ToNumber:    c.ToNumber,
			Channel:     callsession.OfferChannelWhatsApp,
		},
		Candidates:   candidates,
		PerCandidate: whatsappInboundPerAgentRing,
		Deadline:     time.Now().Add(whatsappInboundTotalWindow),
		CallerGone:   termination.gone,
		OnReserved: func(cand callsession.CallSession) {
			if roulette && entryID != "" {
				uc.assignTo(businessPhoneID, workspaceID, entryID, cand.UserID())
			}
		},
	})
	if outcome.CallerGone {
		uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallMissed, "📞 Chamada perdida.")
		return
	}
	if outcome.Session == nil {
		if outcome.DeclinedByUserID != "" {
			uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallMissed,
				"📞 Chamada recusada por "+uc.usernameOf(outcome.DeclinedByUserID)+".")
		} else {
			uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallMissed, "📞 Chamada não atendida.")
		}
		uc.reject(ctx, businessPhoneID, c.CallID)
		return
	}

	reservationActive := true
	defer func() {
		if reservationActive {
			outcome.Session.Release(outcome.OfferID)
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
		select {
		case <-termination.gone:
			call.markEnded(termination.reason)
		case <-call.Done():
		}
	}()

	if err := uc.executor.AttachInboundCRMCall(ctx, callsession.AttachInboundCRMCallInput{
		OfferID:     outcome.OfferID,
		WorkspaceID: workspaceID,
		UserID:      outcome.Session.UserID(),
		Session:     outcome.Session,
		PhoneNumber: c.FromNumber,
		Call:        call,
		Admission:   lease,
		StartedAt:   time.Now(),
	}); err != nil {
		uc.log.Printf("[WAInbound] %s: attach failed: %v", c.CallID, err)
		_ = call.Hangup()
		return
	}
	reservationActive = false
	leaseReleased = true
	closeMedia = false

	if roulette && entryID != "" {
		uc.assignTo(businessPhoneID, workspaceID, entryID, outcome.Session.UserID())
	}

	select {
	case <-ctx.Done():
		_ = call.Hangup()
	case <-call.Done():
	}
	uc.recordEvent(entryID, c.FromNumber, conversation_domain.MessageTypeCallEnded,
		fmt.Sprintf("📞 Chamada encerrada. Duração %s.", formatCallDuration(time.Since(answeredAt))))
}

func (uc *WhatsAppInboundCallUseCase) usernameOf(userID string) string {
	if uc.users != nil {
		if name := strings.TrimSpace(uc.users.ResolveUsernames([]string{userID})[userID]); name != "" {
			return name
		}
	}
	return "um agente"
}

func (uc *WhatsAppInboundCallUseCase) resolveCandidates(workspaceID, departmentID, assignedUserID string) ([]callsession.CallSession, bool) {
	if strings.TrimSpace(assignedUserID) != "" {
		s, ok := uc.sessions.FindByUser(workspaceID, assignedUserID)
		online := ok && s != nil
		busy := online && s.HasActiveCall()
		if online && !busy {
			uc.log.Printf("[WAInbound] resolveCandidates: assigned=%s online=true hasActiveCall=false → 1 candidate (owner-only)", assignedUserID)
			return []callsession.CallSession{s}, false
		}
		uc.log.Printf("[WAInbound] resolveCandidates: assigned=%s online=%t hasActiveCall=%t → 0 candidates (owner-only, no fallback)", assignedUserID, online, busy)
		return nil, false
	}

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
	callSessionUserIDs := make([]string, 0, len(available))
	var candidates []callsession.CallSession
	for _, s := range available {
		if s == nil || s.HasActiveCall() {
			continue
		}
		callSessionUserIDs = append(callSessionUserIDs, s.UserID())
		if allowed[s.UserID()] {
			candidates = append(candidates, s)
		}
	}
	uc.log.Printf("[WAInbound] resolveCandidates: department=%q skipAdmins=%t eligible=%v callSessionAvailable=%v → %d candidate(s)",
		departmentID, skipAdmins, eligible, callSessionUserIDs, len(candidates))
	return candidates, true
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
		SentBy:      conversation_domain.SentByContact(from),
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

type terminationWatch struct {
	gone   chan struct{}
	reason string
}

func watchTermination(signals <-chan conversation_domain.WhatsAppCallSignal) *terminationWatch {
	watch := &terminationWatch{gone: make(chan struct{})}
	go func() {
		for sig := range signals {
			if sig.Kind == conversation_domain.WhatsAppCallTerminate {
				watch.reason = sig.Reason
				close(watch.gone)
				return
			}
		}
	}()
	return watch
}
