package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	cdr "vozko/domain/calls/cdr"
	"vozko/domain/conversation"
	dialer_domain "vozko/domain/dialer"
	"vozko/domain/metrics"
	"vozko/domain/telephony"
	"vozko/infra/http/middleware"
	calls_usecase "vozko/usecases/calls"
	dialer_usecase "vozko/usecases/dialer"
)

type DialerWSHandler struct {
	startUseCase dialer_domain.StartOutboundCallUseCase
	endUseCase   dialer_domain.EndOutboundCallUseCase
	lifecycle    *dialer_usecase.OutboundCallLifecycleRunner
	authorizer   conversation.ConversationAuthorizer
	logger       *log.Logger
	wsMetrics    metrics.WSMetricsRecorder

	sessionRegistry dialer_domain.DialerSessionRegistry
	callRegistry    dialer_domain.DialerCallRegistry
	transferUseCase dialer_domain.CallTransferUseCase
	inboundUseCase  dialer_domain.InboundCallUseCase
	recordingPool   *calls_usecase.RecordingUploadPool

	userResolver TransferUsernameResolver

	// presenceTelemetry records durable on_call/online (queue only; optional).
	presenceTelemetry func(workspaceID, userID, state, source string)

	// boardSync updates Redis live board + returns snapshot for supervisor WS push.
	boardSync telephony.BoardSync
	// capacityReader supplies used/max concurrent call slots for the board bar.
	capacityReader telephony.CapacityReader

	// presence broadcast is coalesced per workspace: a burst of changes (a transfer
	// reserving then cancelling several contacts, many agents reconnecting) collapses
	// into ONE debounced push, and the DB-backed snapshot build + fan-out runs off the
	// caller's goroutine so it never blocks the transfer/call hot paths.
	presenceMu      sync.Mutex
	presencePending map[string]bool
}

// presenceBroadcastDebounce coalesces a burst of presence changes into one push. Short
// enough to feel real time, long enough that a multi-contact transfer's reserve/cancel
// storm becomes a single snapshot.
const presenceBroadcastDebounce = 150 * time.Millisecond

type TransferUsernameResolver interface {
	ResolveUsernames(userIDs []string) map[string]string
}

var dialerForcedShutdownTimeout = 3 * time.Second

func NewDialerWSHandler(
	startUseCase dialer_domain.StartOutboundCallUseCase,
	endUseCase dialer_domain.EndOutboundCallUseCase,
	lifecycle *dialer_usecase.OutboundCallLifecycleRunner,
	authorizer conversation.ConversationAuthorizer,
	logger *log.Logger,
	wsMetrics metrics.WSMetricsRecorder,
) *DialerWSHandler {
	if logger == nil {
		logger = log.Default()
	}
	return &DialerWSHandler{
		startUseCase: startUseCase,
		endUseCase:   endUseCase,
		lifecycle:    lifecycle,
		authorizer:   authorizer,
		logger:       logger,
		wsMetrics:    wsMetrics,
	}
}

func (h *DialerWSHandler) WithTransfer(
	sessions dialer_domain.DialerSessionRegistry,
	calls dialer_domain.DialerCallRegistry,
	useCase dialer_domain.CallTransferUseCase,
) *DialerWSHandler {
	if sessions == nil || calls == nil || useCase == nil {
		panic("DialerWSHandler.WithTransfer: all three ports are required")
	}
	h.sessionRegistry = sessions
	h.callRegistry = calls
	h.transferUseCase = useCase
	sessions.SetPresenceListener(h)
	return h
}

func (h *DialerWSHandler) WithUserResolver(resolver TransferUsernameResolver) *DialerWSHandler {
	h.userResolver = resolver
	return h
}

// WithPresenceTelemetry wires durable on_call/online telemetry (queue-backed).
func (h *DialerWSHandler) WithPresenceTelemetry(fn func(workspaceID, userID, state, source string)) *DialerWSHandler {
	h.presenceTelemetry = fn
	return h
}

// WithLiveBoard wires Redis live concurrency board sync + capacity reader.
func (h *DialerWSHandler) WithLiveBoard(sync telephony.BoardSync, capacity telephony.CapacityReader) *DialerWSHandler {
	h.boardSync = sync
	h.capacityReader = capacity
	return h
}

func (h *DialerWSHandler) WithInboundCalls(useCase dialer_domain.InboundCallUseCase) *DialerWSHandler {
	h.inboundUseCase = useCase
	return h
}

func (h *DialerWSHandler) WithRecording(pool *calls_usecase.RecordingUploadPool) *DialerWSHandler {
	h.recordingPool = pool
	return h
}

func (h *DialerWSHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.startUseCase == nil || h.endUseCase == nil {
		http.Error(w, "Dialer websocket not configured", http.StatusNotImplemented)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	workspaceID := middleware.GetWorkspaceID(r)
	if qsWS := r.URL.Query().Get("workspaceId"); qsWS != "" {
		workspaceID = qsWS
	}
	if workspaceID == "" {
		http.Error(w, "workspace is required", http.StatusForbidden)
		return
	}

	isAdmin := strings.TrimSpace(claims.Role) == "admin"

	ws, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Printf("[DialerWS] upgrade error: %v", err)
		return
	}
	defer ws.Close()

	h.wsMetrics.IncWSConnections(metrics.WSEndpointDialer)
	defer h.wsMetrics.DecWSConnections(metrics.WSEndpointDialer)

	var writeMu sync.Mutex
	send := func(msg *WSOutgoingMessage) {
		if msg == nil {
			return
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = ws.WriteJSON(msg)
	}

	send(&WSOutgoingMessage{
		Type: WSEventConnected,
		Payload: map[string]string{
			"feature":      "dialer",
			"workspace_id": workspaceID,
			"user_id":      claims.UserID,
		},
	})

	session := newDialerSession(
		uuid.New().String(),
		claims.UserID,
		workspaceID,
		send,
		h.endUseCase,
		h.logger,
		0,
	)
	if h.presenceTelemetry != nil {
		session.SetPresenceTelemetry(h.presenceTelemetry)
	}

	if h.sessionRegistry != nil {

		registry := h.sessionRegistry
		ws := workspaceID
		session.SetPresenceCallback(func() { registry.NotifyPresenceChanged(ws) })
		deregister, err := h.sessionRegistry.Register(session)
		if err != nil {
			h.logger.Printf("[DialerWS] session registry rejected session: %v", err)
		} else {
			defer deregister()
		}
	}

	for {
		_, msgBytes, err := ws.ReadMessage()
		if err != nil {
			break
		}
		var in WSIncomingMessage
		if err := json.Unmarshal(msgBytes, &in); err != nil {
			send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "invalid_payload", Message: "Invalid websocket message"}})
			continue
		}

		switch in.Type {
		case WSEventStartCall:
			var p StartCallPayload
			if err := json.Unmarshal(in.Payload, &p); err != nil {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "invalid_payload", Message: "Invalid start_call payload"}})
				continue
			}
			if strings.TrimSpace(p.PhoneNumber) == "" {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "missing_fields", Message: "phone_number is required"}})
				continue
			}

			if h.authorizer != nil && !h.authorizer.HasWorkspacePermission(claims.UserID, workspaceID, "dialer", "use", false) {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "unauthorized", Message: "You don't have permission to use the dialer"}})
				continue
			}

			if session.HasActiveCall() {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "already_in_call", Message: "You already have an active call. End it first."}})
				continue
			}

			started, err := h.startUseCase.Execute(context.Background(), dialer_domain.StartOutboundCallInput{
				WorkspaceID:     workspaceID,
				UserID:          claims.UserID,
				IsAdmin:         isAdmin,
				TargetPhone:     p.PhoneNumber,
				SIPTrunkID:      p.SIPTrunkID,
				WhatsAppPhoneID: p.WhatsAppPhoneID,
				OnWaitingForSlot: func() {
					send(&WSOutgoingMessage{Type: WSEventWaitingCallSlot, Payload: WaitingCallSlotPayload{Reason: "All call slots in use, waiting for one to free up"}})
				},
			})
			if err != nil {
				if busy := dialer_domain.AsTrunkBusy(err); busy != nil {
					send(buildCallTrunkBusyMessage(busy, callTrunkBusyContext{
						PhoneNumber: strings.TrimSpace(p.PhoneNumber),
						RequestID:   p.RequestID,
					}))
					continue
				}
				h.sendStartCallError(send, err)
				continue
			}

			if _, err := attachDialerCall(context.Background(), dialerCallAttachInput{
				Session:       session,
				Call:          started.Call,
				Admission:     started.Admission,
				Phone:         started.PhoneNumber,
				RequestID:     p.RequestID,
				WorkspaceID:   workspaceID,
				OwnerUserID:   claims.UserID,
				SIPTrunkID:    p.SIPTrunkID,
				StartedAt:     time.Now(),
				Direction:     cdr.DirectionOutbound,
				CallRegistry:  h.callRegistry,
				EndUseCase:    h.endUseCase,
				Lifecycle:     h.lifecycle,
				RecordingPool: h.recordingPool,
				Logger:        h.logger,
				OnCallGone:    h.transferLegDeathHook(),
			}); err != nil {
				h.logger.Printf("[DialerWS] attach error: %v", err)
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "dial_failed", Message: "Failed to initiate call"}})
				continue
			}
		case WSEventEndCall:
			lc := session.Current()
			if lc == nil {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "no_active_call", Message: "No active call to end"}})
				continue
			}
			// During a consult the red button ends the CONSULT leg, not the customer's
			// call (plan D2): cancel the transfer so the caller comes off hold and
			// resumes with this agent. Explicitly hanging up on the customer is then a
			// second EndCall with no transfer in flight.
			if h.transferUseCase != nil {
				if th, ok := h.transferUseCase.FindActiveByCall(session.workspaceID, lc.call.ID()); ok &&
					th.Stage == dialer_domain.TransferStageConsulting && th.InitiatorID == session.userID {
					if err := h.transferUseCase.CancelAttended(context.Background(), th.ID, session.userID, "initiator_ended_call"); err != nil {
						h.sendTransferError(session, "", th.ID, err)
					}
					continue
				}
			}
			// Hanging up mid-transfer must also abort a pending offer, so every ringing
			// contact stops and their reservations are freed (otherwise the target
			// keeps ringing and stays "busy" in the picker).
			if h.transferUseCase != nil {
				_ = h.transferUseCase.AbortByCall(context.Background(), session.workspaceID, lc.call.ID(), "initiator_ended_call")
			}
			_ = h.endUseCase.Execute(context.Background(), dialer_domain.EndOutboundCallInput{Call: lc.call, Hangup: true})
		case WSEventCallAudio:
			var p CallAudioPayload
			if err := json.Unmarshal(in.Payload, &p); err != nil {
				continue
			}
			pcm, err := base64.StdEncoding.DecodeString(p.Audio)
			if err != nil {
				continue
			}
			if p.SampleRate != 0 && p.SampleRate != sipDefaultSampleRate {
				converted, ok := func() ([]byte, bool) {
					if lc := session.Current(); lc != nil {
						return lc.inboundConverter.Convert(pcm, p.SampleRate)
					}
					return nil, false
				}()
				if !ok {
					if h.logger != nil {
						h.logger.Printf("[DialerWS] dropping call_audio with unsupported sample_rate=%d", p.SampleRate)
					}
					continue
				}
				pcm = converted
			}

			if endpoint := session.ConsultEndpoint(); endpoint != nil {
				endpoint.Send(pcm)
				continue
			}
			lc := session.Current()
			if lc == nil {
				continue
			}
			// While the caller is held (transfer ring window) the hold player is the
			// leg's SOLE writer: drop the mic uplink so agent frames never interleave
			// with the hold audio (the caller must hear music, not the agent).
			if lc.holdActive.Load() {
				continue
			}

			lc.enqueueAudio(pcm)
		case WSEventTransferInitiate:
			h.handleTransferInitiate(session, in.Payload)
		case WSEventTransferAccept:
			h.handleTransferAction(session, in.Payload, transferActionAccept)
		case WSEventTransferDecline:
			h.handleTransferAction(session, in.Payload, transferActionDecline)
		case WSEventTransferComplete:
			h.handleTransferAction(session, in.Payload, transferActionComplete)
		case WSEventTransferCancel:
			h.handleTransferAction(session, in.Payload, transferActionCancel)
		case WSEventTransferListTargets:
			h.handleTransferListTargets(session)
		case WSEventInboundCallAccept:
			h.handleInboundCallAction(session, in.Payload, true)
		case WSEventInboundCallDecline:
			h.handleInboundCallAction(session, in.Payload, false)
		}
	}

	// The agent's socket dropped: if they had a transfer offer in flight, abort it so
	// every ringing contact (browser popup / branch INVITE) stops and reservations are
	// freed instead of leaking until the reaper times the handle out.
	if h.transferUseCase != nil {
		if lc := session.Current(); lc != nil {
			_ = h.transferUseCase.AbortByCall(context.Background(), session.workspaceID, lc.call.ID(), "initiator_disconnect")
		}
	}

	session.Shutdown(context.Background())
}

// transferLegDeathHook builds the customer-hangup funnel callback for calls this
// handler attaches: when the caller's leg dies, any in-flight transfer for it is
// aborted instantly (popups close, reservations free, consults tear down).
func (h *DialerWSHandler) transferLegDeathHook() func(workspaceID, callID string) {
	if h.transferUseCase == nil {
		return nil
	}
	uc := h.transferUseCase
	return func(workspaceID, callID string) {
		_ = uc.AbortByCallLegDeath(context.Background(), workspaceID, callID)
	}
}

func (h *DialerWSHandler) sendStartCallError(send func(*WSOutgoingMessage), err error) {
	if send == nil {
		return
	}
	switch {
	case err == nil:
		return
	case strings.Contains(err.Error(), dialer_domain.ErrNoCallSlotsAvailable.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "no_call_slots", Message: "No call slots available, please try again shortly"}})
	case strings.Contains(err.Error(), dialer_domain.ErrInsufficientBalance.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "insufficient_balance", Message: "Insufficient balance to start a call"}})
	case strings.Contains(err.Error(), dialer_domain.ErrTargetPhoneRequired.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "missing_fields", Message: "phone_number is required"}})
	case strings.Contains(err.Error(), dialer_domain.ErrCallSourceNotConfigured.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "not_configured", Message: "Call source not configured"}})
	case errors.Is(err, conversation.ErrWhatsAppCallNoPermission):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "whatsapp_permission_required", Message: "The customer hasn't granted permission to receive WhatsApp calls"}})
	default:
		h.logger.Printf("[DialerWS] start call error: %v", err)
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "dial_failed", Message: "Failed to initiate call"}})
	}
}

func (h *DialerWSHandler) handleInboundCallAction(session *dialerSession, raw json.RawMessage, accept bool) {
	if h.inboundUseCase == nil {
		session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "inbound_unavailable", Message: "Inbound calls are not enabled on this server"}})
		return
	}
	var payload InboundCallActionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "invalid_payload", Message: "Invalid incoming call payload"}})
		return
	}
	offerID := strings.TrimSpace(payload.OfferID)
	if offerID == "" {
		session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "missing_fields", Message: "offer_id is required"}})
		return
	}

	var err error
	if accept {
		err = h.inboundUseCase.Accept(context.Background(), dialer_domain.AcceptInboundCallInput{
			OfferID:     offerID,
			WorkspaceID: session.workspaceID,
			UserID:      session.userID,
			SessionID:   session.ID(),
		})
	} else {
		err = h.inboundUseCase.Decline(context.Background(), dialer_domain.DeclineInboundCallInput{
			OfferID:     offerID,
			WorkspaceID: session.workspaceID,
			UserID:      session.userID,
			SessionID:   session.ID(),
			Reason:      payload.Reason,
		})
	}
	if err != nil {
		h.sendInboundCallError(session, offerID, err)
	}
}

func (h *DialerWSHandler) sendInboundCallError(session *dialerSession, offerID string, err error) {
	if err == nil {
		return
	}
	code := "inbound_failed"
	message := "Incoming call failed"
	switch {
	case errors.Is(err, dialer_domain.ErrInboundOfferNotFound):
		code, message = "offer_not_found", "Incoming call offer is no longer available"
	case errors.Is(err, dialer_domain.ErrInboundOfferNotForUser):
		code, message = "not_for_user", "This incoming call is not addressed to you"
	case errors.Is(err, dialer_domain.ErrInboundOfferAlreadyResolved):
		code, message = "offer_resolved", "Incoming call offer was already answered"
	case errors.Is(err, dialer_domain.ErrTransferTargetBusy):
		code, message = "already_in_call", "You already have an active call"
	default:
		h.logger.Printf("[DialerWS] inbound call error: %v", err)
	}
	session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: code, Message: message, EntryID: offerID}})
}

type transferAction int

const (
	transferActionAccept transferAction = iota
	transferActionDecline
	transferActionComplete
	transferActionCancel
)

func (h *DialerWSHandler) handleTransferInitiate(session *dialerSession, raw json.RawMessage) {
	if h.transferUseCase == nil {
		session.send(&WSOutgoingMessage{Type: WSEventTransferError, Payload: TransferErrorPayload{Code: "transfer_unavailable", Message: "Transfer is not enabled on this server"}})
		return
	}
	var p TransferInitiatePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		session.send(&WSOutgoingMessage{Type: WSEventTransferError, Payload: TransferErrorPayload{Code: "invalid_payload", Message: "Invalid transfer:initiate payload"}})
		return
	}
	kind := dialer_domain.TransferKind(strings.TrimSpace(strings.ToLower(p.Kind)))
	if kind == "" {
		kind = dialer_domain.TransferKindBlind
	}

	if h.authorizer != nil && !h.authorizer.HasWorkspacePermission(session.userID, session.workspaceID, "dialer", "transfer", false) {
		session.send(&WSOutgoingMessage{Type: WSEventTransferError, Payload: TransferErrorPayload{Code: "unauthorized", Message: "You don't have permission to transfer calls"}})
		return
	}

	handle, err := h.transferUseCase.Initiate(context.Background(), dialer_domain.TransferRequest{
		WorkspaceID:  session.workspaceID,
		InitiatorID:  session.userID,
		TargetUserID: strings.TrimSpace(p.TargetUserID),
		CallID:       strings.TrimSpace(p.CallID),
		Kind:         kind,
		Note:         p.Note,
	})
	if err != nil {
		h.sendTransferError(session, p.CallID, "", err)
		return
	}

	session.send(&WSOutgoingMessage{
		Type: WSEventTransferStarted,
		Payload: map[string]any{
			"transfer_id":    handle.ID,
			"call_id":        handle.CallID,
			"target_user_id": handle.TargetUserID,
			"kind":           string(handle.Kind),
			"stage":          string(handle.Stage),
		},
	})
}

func (h *DialerWSHandler) handleTransferAction(session *dialerSession, raw json.RawMessage, action transferAction) {
	if h.transferUseCase == nil {
		session.send(&WSOutgoingMessage{Type: WSEventTransferError, Payload: TransferErrorPayload{Code: "transfer_unavailable", Message: "Transfer is not enabled on this server"}})
		return
	}
	var p TransferActionPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		session.send(&WSOutgoingMessage{Type: WSEventTransferError, Payload: TransferErrorPayload{Code: "invalid_payload", Message: "Invalid transfer payload"}})
		return
	}
	tid := strings.TrimSpace(p.TransferID)
	if tid == "" {
		session.send(&WSOutgoingMessage{Type: WSEventTransferError, Payload: TransferErrorPayload{Code: "missing_fields", Message: "transfer_id is required"}})
		return
	}

	requiredAction := "transfer"
	if action == transferActionAccept || action == transferActionDecline {
		requiredAction = "use"
	}
	if h.authorizer != nil && !h.authorizer.HasWorkspacePermission(session.userID, session.workspaceID, "dialer", requiredAction, false) {
		session.send(&WSOutgoingMessage{Type: WSEventTransferError, Payload: TransferErrorPayload{TransferID: tid, Code: "unauthorized", Message: "You don't have permission to manage transfers"}})
		return
	}

	ctx := context.Background()
	var err error
	switch action {
	case transferActionAccept:
		// This browser contact accepted; its session id is the winner so the swap
		// lands here and the member's other contacts (e.g. their branch) are cancelled.
		_, err = h.transferUseCase.Accept(ctx, tid, session.userID, session.ID())
	case transferActionDecline:
		err = h.transferUseCase.Decline(ctx, tid, session.userID, p.Reason)
	case transferActionComplete:
		_, err = h.transferUseCase.CompleteAttended(ctx, tid, session.userID)
	case transferActionCancel:
		err = h.transferUseCase.CancelAttended(ctx, tid, session.userID, p.Reason)
	}
	if err != nil {
		h.sendTransferError(session, "", tid, err)
	}
}

func (h *DialerWSHandler) sendTransferError(session *dialerSession, callID, transferID string, err error) {
	if err == nil {
		return
	}
	code := "transfer_failed"
	message := "Transfer failed"

	underlying := err
	var te *dialer_domain.TransferError
	if errors.As(err, &te) && te != nil {
		underlying = te.Err
	}

	switch {
	case errors.Is(underlying, dialer_domain.ErrTransferTargetOffline):
		code, message = "target_offline", "The selected member is offline"
	case errors.Is(underlying, dialer_domain.ErrTransferTargetBusy):
		code, message = "target_busy", "The selected member is already on a call"
	case errors.Is(underlying, dialer_domain.ErrTransferSelfTransfer):
		code, message = "self_transfer", "You cannot transfer a call to yourself"
	case errors.Is(underlying, dialer_domain.ErrTransferCallNotFound):
		code, message = "call_not_found", "Call no longer exists"
	case errors.Is(underlying, dialer_domain.ErrTransferNotOwner):
		code, message = "not_owner", "You do not own this call"
	case errors.Is(underlying, dialer_domain.ErrTransferNotFound):
		code, message = "transfer_not_found", "Transfer offer is no longer available"
	case errors.Is(underlying, dialer_domain.ErrTransferNotForUser):
		code, message = "not_for_user", "This transfer is not addressed to you"
	case errors.Is(underlying, dialer_domain.ErrTransferAlreadyInFlight):
		code, message = "already_in_flight", "Another transfer is already in progress for this call"
	case errors.Is(underlying, dialer_domain.ErrTransferTargetRequired):
		code, message = "missing_fields", "target_user_id is required"
	case errors.Is(underlying, dialer_domain.ErrTransferInvalidKind):
		code, message = "invalid_kind", "Unsupported transfer kind"
	case errors.Is(underlying, dialer_domain.ErrTransferInvalidStage):
		code, message = "invalid_stage", "Transfer is not in a state that accepts this action"
	case errors.Is(underlying, dialer_domain.ErrTransferTimedOut):
		code, message = "timed_out", "Transfer offer expired"
	default:
		h.logger.Printf("[DialerWS] transfer error: %v", err)
	}
	session.send(&WSOutgoingMessage{
		Type: WSEventTransferError,
		Payload: TransferErrorPayload{
			TransferID: transferID,
			CallID:     callID,
			Code:       code,
			Message:    message,
		},
	})
}

func (h *DialerWSHandler) handleTransferListTargets(session *dialerSession) {
	if h.transferUseCase == nil || h.sessionRegistry == nil {
		session.send(&WSOutgoingMessage{Type: WSEventTransferError, Payload: TransferErrorPayload{Code: "transfer_unavailable", Message: "Transfer is not enabled on this server"}})
		return
	}
	if h.authorizer != nil && !h.authorizer.HasWorkspacePermission(session.userID, session.workspaceID, "dialer", "list_members", false) {
		session.send(&WSOutgoingMessage{Type: WSEventTransferError, Payload: TransferErrorPayload{Code: "unauthorized", Message: "You don't have permission to list transfer targets"}})
		return
	}
	available := h.sessionRegistry.ListAvailable(session.workspaceID)
	users := make([]TransferTargetUser, 0, len(available))
	ids := make([]string, 0, len(available))
	availIDs := make([]string, 0, len(available))
	for _, s := range available {
		if s == nil {
			continue
		}
		availIDs = append(availIDs, s.UserID()+"("+s.ID()+")")
		if s.UserID() == session.userID {
			continue // never offer a member their own session
		}
		ids = append(ids, s.UserID())
		users = append(users, TransferTargetUser{UserID: s.UserID()})
	}
	// Diagnostic: reveals whether an empty picker is "no other member has a live
	// contact" vs "the only contact is the requester's own" (self-filtered).
	if h.logger != nil {
		h.logger.Printf("[Transfer] list_targets requester=%s ws=%s available=%v -> %d target(s)",
			session.userID, session.workspaceID, availIDs, len(users))
	}

	if h.userResolver != nil && len(ids) > 0 {
		names := h.userResolver.ResolveUsernames(ids)
		for i := range users {
			if name, ok := names[users[i].UserID]; ok {
				users[i].Username = name
			}
		}
	}
	session.send(&WSOutgoingMessage{
		Type:    WSEventTransferTargets,
		Payload: TransferTargetsPayload{Users: users},
	})
}

// OnPresenceChanged is the PresenceListener hook. It is called synchronously from the
// registry on every presence change (connect/disconnect/call attach/detach, ring
// reserve/release), including from latency-sensitive transfer paths, so it must return
// fast. It schedules a coalesced, async broadcast rather than doing the DB-backed
// snapshot build + fan-out inline: a burst of changes within the debounce window
// collapses to a single push that reads the latest state.
func (h *DialerWSHandler) OnPresenceChanged(workspaceID string) {
	if h == nil || h.sessionRegistry == nil || workspaceID == "" {
		return
	}
	h.presenceMu.Lock()
	if h.presencePending == nil {
		h.presencePending = make(map[string]bool)
	}
	if h.presencePending[workspaceID] {
		h.presenceMu.Unlock()
		return // a broadcast is already scheduled; it will pick up this change too
	}
	h.presencePending[workspaceID] = true
	h.presenceMu.Unlock()

	go func() {
		time.Sleep(presenceBroadcastDebounce)
		h.presenceMu.Lock()
		delete(h.presencePending, workspaceID)
		h.presenceMu.Unlock()
		h.broadcastPresence(workspaceID)
	}()
}

// broadcastPresence builds the live presence snapshot (one row per online member with
// status + endpoint kinds) and pushes it to every BROWSER session in the workspace,
// permission-gated: a viewer with dialer:list_members sees everyone, otherwise they see
// only themselves. Also refreshes the Redis live board and pushes telephony:board to
// supervisors (list_members).
func (h *DialerWSHandler) broadcastPresence(workspaceID string) {
	if h == nil || h.sessionRegistry == nil || workspaceID == "" {
		return
	}

	presence := h.sessionRegistry.ListPresence(workspaceID)
	users := make([]DialerPresenceUser, 0, len(presence))
	ids := make([]string, 0, len(presence))
	seats := make([]telephony.HumanSeat, 0, len(presence))
	now := time.Now().UTC()
	for _, p := range presence {
		ids = append(ids, p.UserID)
		users = append(users, DialerPresenceUser{
			UserID:     p.UserID,
			Busy:       p.Busy,
			OnCall:     p.OnCall,
			Ringing:    p.Ringing,
			HasBrowser: p.HasBrowser,
			HasBranch:  p.HasBranch,
		})
		state := telephony.SeatFree
		switch {
		case p.OnCall:
			state = telephony.SeatOnCall
		case p.Ringing:
			state = telephony.SeatRinging
		case p.Busy:
			state = telephony.SeatOnCall
		}
		seats = append(seats, telephony.HumanSeat{
			UserID:     p.UserID,
			State:      state,
			HasBrowser: p.HasBrowser,
			HasBranch:  p.HasBranch,
			Since:      now,
		})
	}
	if h.userResolver != nil && len(ids) > 0 {
		names := h.userResolver.ResolveUsernames(ids)
		for i := range users {
			if name, ok := names[users[i].UserID]; ok {
				users[i].Username = name
			}
		}
		for i := range seats {
			if name, ok := names[seats[i].UserID]; ok {
				seats[i].Username = name
			}
		}
	}

	// Live board: Redis only (never blocks on SQL).
	var boardSnap *telephony.BoardSnapshot
	if h.boardSync != nil {
		var used, max int64
		if h.capacityReader != nil {
			used, max, _ = h.capacityReader.Snapshot(workspaceID)
		}
		if snap, err := h.boardSync.SyncHumansFromPresence(workspaceID, seats, used, max); err == nil {
			boardSnap = snap
		}
	}

	recipients := h.sessionRegistry.ListBrowserSessions(workspaceID)
	if len(recipients) == 0 {
		return
	}

	fullMsg := dialer_domain.DialerControlMessage{
		Type:    string(WSEventDialerPresence),
		Payload: DialerPresencePayload{Users: users},
	}
	for _, s := range recipients {
		if s == nil {
			continue
		}
		canList := h.authorizer == nil || h.authorizer.HasWorkspacePermission(s.UserID(), workspaceID, "dialer", "list_members", false)
		msg := fullMsg
		if !canList {
			selfOnly := make([]DialerPresenceUser, 0, 1)
			for _, u := range users {
				if u.UserID == s.UserID() {
					selfOnly = append(selfOnly, u)
					break
				}
			}
			msg = dialer_domain.DialerControlMessage{
				Type:    string(WSEventDialerPresence),
				Payload: DialerPresencePayload{Users: selfOnly},
			}
		}
		if err := s.Notify(msg); err != nil {
			h.logger.Printf("[DialerWS] presence notify session=%s user=%s: %v", s.ID(), s.UserID(), err)
		}
		// Supervisors get the full concurrency board (squares + capacity + AI).
		if canList && boardSnap != nil {
			_ = s.Notify(dialer_domain.DialerControlMessage{
				Type:    string(WSEventTelephonyBoard),
				Payload: boardSnap,
			})
		}
	}
}
