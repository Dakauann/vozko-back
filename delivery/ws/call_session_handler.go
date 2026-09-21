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
	callsession_domain "vozko/domain/callsession"
	"vozko/domain/conversation"
	"vozko/domain/metrics"
	"vozko/domain/telephony"
	"vozko/infra/http/middleware"
	calls_usecase "vozko/usecases/calls"
	callsession_usecase "vozko/usecases/callsession"
)

type CallSessionWSHandler struct {
	startUseCase callsession_domain.StartOutboundCallUseCase
	endUseCase   callsession_domain.EndOutboundCallUseCase
	lifecycle    *callsession_usecase.OutboundCallLifecycleRunner
	authorizer   conversation.ConversationAuthorizer
	logger       *log.Logger
	wsMetrics    metrics.WSMetricsRecorder

	sessionRegistry callsession_domain.CallSessionRegistry
	callRegistry    callsession_domain.CallRegistry
	inboundOffers   callsession_domain.InboundOfferResponder
	recordingPool   *calls_usecase.RecordingUploadPool

	userResolver TransferUsernameResolver

	presenceTelemetry func(workspaceID, userID, state, source string)

	boardSync      telephony.BoardSync
	capacityReader telephony.CapacityReader

	presenceMu      sync.Mutex
	presencePending map[string]bool
}

const presenceBroadcastDebounce = 150 * time.Millisecond

type TransferUsernameResolver interface {
	ResolveUsernames(userIDs []string) map[string]string
}

var callSessionForcedShutdownTimeout = 3 * time.Second

func NewCallSessionWSHandler(
	startUseCase callsession_domain.StartOutboundCallUseCase,
	endUseCase callsession_domain.EndOutboundCallUseCase,
	lifecycle *callsession_usecase.OutboundCallLifecycleRunner,
	authorizer conversation.ConversationAuthorizer,
	logger *log.Logger,
	wsMetrics metrics.WSMetricsRecorder,
) *CallSessionWSHandler {
	if logger == nil {
		logger = log.Default()
	}
	return &CallSessionWSHandler{
		startUseCase: startUseCase,
		endUseCase:   endUseCase,
		lifecycle:    lifecycle,
		authorizer:   authorizer,
		logger:       logger,
		wsMetrics:    wsMetrics,
	}
}

func (h *CallSessionWSHandler) WithRegistries(
	sessions callsession_domain.CallSessionRegistry,
	calls callsession_domain.CallRegistry,
) *CallSessionWSHandler {
	if sessions == nil || calls == nil {
		panic("CallSessionWSHandler.WithRegistries: both registries are required")
	}
	h.sessionRegistry = sessions
	h.callRegistry = calls
	sessions.SetPresenceListener(h)
	return h
}

func (h *CallSessionWSHandler) WithUserResolver(resolver TransferUsernameResolver) *CallSessionWSHandler {
	h.userResolver = resolver
	return h
}

func (h *CallSessionWSHandler) WithPresenceTelemetry(fn func(workspaceID, userID, state, source string)) *CallSessionWSHandler {
	h.presenceTelemetry = fn
	return h
}

func (h *CallSessionWSHandler) WithLiveBoard(sync telephony.BoardSync, capacity telephony.CapacityReader) *CallSessionWSHandler {
	h.boardSync = sync
	h.capacityReader = capacity
	return h
}

func (h *CallSessionWSHandler) WithInboundCalls(offers callsession_domain.InboundOfferResponder) *CallSessionWSHandler {
	h.inboundOffers = offers
	return h
}

func (h *CallSessionWSHandler) WithRecording(pool *calls_usecase.RecordingUploadPool) *CallSessionWSHandler {
	h.recordingPool = pool
	return h
}

func (h *CallSessionWSHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.startUseCase == nil || h.endUseCase == nil {
		http.Error(w, "Call session websocket not configured", http.StatusNotImplemented)
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
		h.logger.Printf("[CallSessionWS] upgrade error: %v", err)
		return
	}
	defer ws.Close()

	h.wsMetrics.IncWSConnections(metrics.WSEndpointCallSession)
	defer h.wsMetrics.DecWSConnections(metrics.WSEndpointCallSession)

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
			"feature":      "call-session",
			"workspace_id": workspaceID,
			"user_id":      claims.UserID,
		},
	})

	session := newCallSession(
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
			h.logger.Printf("[CallSessionWS] session registry rejected session: %v", err)
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

			if h.authorizer != nil && !h.authorizer.HasWorkspacePermission(claims.UserID, workspaceID, "call_session", "use", false) {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "unauthorized", Message: "You don't have permission to place calls"}})
				continue
			}

			if session.HasActiveCall() {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "already_in_call", Message: "You already have an active call. End it first."}})
				continue
			}

			started, err := h.startUseCase.Execute(context.Background(), callsession_domain.StartOutboundCallInput{
				WorkspaceID:     workspaceID,
				UserID:          claims.UserID,
				IsAdmin:         isAdmin,
				TargetPhone:     p.PhoneNumber,
				WhatsAppPhoneID: p.WhatsAppPhoneID,
				OnWaitingForSlot: func() {
					send(&WSOutgoingMessage{Type: WSEventWaitingCallSlot, Payload: WaitingCallSlotPayload{Reason: "All call slots in use, waiting for one to free up"}})
				},
			})
			if err != nil {
				h.sendStartCallError(send, err)
				continue
			}

			if _, err := attachCall(context.Background(), callAttachInput{
				Session:       session,
				Call:          started.Call,
				Admission:     started.Admission,
				Phone:         started.PhoneNumber,
				RequestID:     p.RequestID,
				WorkspaceID:   workspaceID,
				OwnerUserID:   claims.UserID,
				StartedAt:     time.Now(),
				Direction:     cdr.DirectionOutbound,
				CallRegistry:  h.callRegistry,
				EndUseCase:    h.endUseCase,
				Lifecycle:     h.lifecycle,
				RecordingPool: h.recordingPool,
				Logger:        h.logger,
			}); err != nil {
				h.logger.Printf("[CallSessionWS] attach error: %v", err)
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "dial_failed", Message: "Failed to initiate call"}})
				continue
			}
		case WSEventEndCall:
			lc := session.Current()
			if lc == nil {
				send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "no_active_call", Message: "No active call to end"}})
				continue
			}
			_ = h.endUseCase.Execute(context.Background(), callsession_domain.EndOutboundCallInput{Call: lc.call, Hangup: true})
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
						h.logger.Printf("[CallSessionWS] dropping call_audio with unsupported sample_rate=%d", p.SampleRate)
					}
					continue
				}
				pcm = converted
			}

			lc := session.Current()
			if lc == nil {
				continue
			}
			lc.enqueueAudio(pcm)
		case WSEventInboundCallAccept:
			h.handleInboundCallAction(session, in.Payload, true)
		case WSEventInboundCallDecline:
			h.handleInboundCallAction(session, in.Payload, false)
		}
	}

	session.Shutdown(context.Background())
}

func (h *CallSessionWSHandler) sendStartCallError(send func(*WSOutgoingMessage), err error) {
	if send == nil {
		return
	}
	switch {
	case err == nil:
		return
	case strings.Contains(err.Error(), callsession_domain.ErrNoCallSlotsAvailable.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "no_call_slots", Message: "No call slots available, please try again shortly"}})
	case strings.Contains(err.Error(), callsession_domain.ErrInsufficientBalance.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "insufficient_balance", Message: "Insufficient balance to start a call"}})
	case strings.Contains(err.Error(), callsession_domain.ErrTargetPhoneRequired.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "missing_fields", Message: "phone_number is required"}})
	case strings.Contains(err.Error(), callsession_domain.ErrCallSourceNotConfigured.Error()):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "not_configured", Message: "Call source not configured"}})
	case errors.Is(err, conversation.ErrWhatsAppCallNoPermission):
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "whatsapp_permission_required", Message: "The customer hasn't granted permission to receive WhatsApp calls"}})
	default:
		h.logger.Printf("[CallSessionWS] start call error: %v", err)
		send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: "dial_failed", Message: "Failed to initiate call"}})
	}
}

func (h *CallSessionWSHandler) handleInboundCallAction(session *callSession, raw json.RawMessage, accept bool) {
	if h.inboundOffers == nil {
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
		err = h.inboundOffers.Accept(context.Background(), callsession_domain.AcceptInboundCallInput{
			OfferID:     offerID,
			WorkspaceID: session.workspaceID,
			UserID:      session.userID,
			SessionID:   session.ID(),
		})
	} else {
		err = h.inboundOffers.Decline(context.Background(), callsession_domain.DeclineInboundCallInput{
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

func (h *CallSessionWSHandler) sendInboundCallError(session *callSession, offerID string, err error) {
	if err == nil {
		return
	}
	code := "inbound_failed"
	message := "Incoming call failed"
	switch {
	case errors.Is(err, callsession_domain.ErrInboundOfferNotFound):
		code, message = "offer_not_found", "Incoming call offer is no longer available"
	case errors.Is(err, callsession_domain.ErrInboundOfferNotForUser):
		code, message = "not_for_user", "This incoming call is not addressed to you"
	case errors.Is(err, callsession_domain.ErrInboundOfferAlreadyResolved):
		code, message = "offer_resolved", "Incoming call offer was already answered"
	case errors.Is(err, callsession_domain.ErrSessionBusy):
		code, message = "already_in_call", "You already have an active call"
	default:
		h.logger.Printf("[CallSessionWS] inbound call error: %v", err)
	}
	session.send(&WSOutgoingMessage{Type: WSEventError, Payload: ErrorPayload{Code: code, Message: message, EntryID: offerID}})
}

func (h *CallSessionWSHandler) OnPresenceChanged(workspaceID string) {
	if h == nil || h.sessionRegistry == nil || workspaceID == "" {
		return
	}
	h.presenceMu.Lock()
	if h.presencePending == nil {
		h.presencePending = make(map[string]bool)
	}
	if h.presencePending[workspaceID] {
		h.presenceMu.Unlock()
		return
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

func (h *CallSessionWSHandler) broadcastPresence(workspaceID string) {
	if h == nil || h.sessionRegistry == nil || workspaceID == "" {
		return
	}

	presence := h.sessionRegistry.ListPresence(workspaceID)
	users := make([]CallSessionPresenceUser, 0, len(presence))
	ids := make([]string, 0, len(presence))
	seats := make([]telephony.HumanSeat, 0, len(presence))
	now := time.Now().UTC()
	for _, p := range presence {
		ids = append(ids, p.UserID)
		users = append(users, CallSessionPresenceUser{
			UserID:     p.UserID,
			Busy:       p.Busy,
			OnCall:     p.OnCall,
			Ringing:    p.Ringing,
			HasBrowser: p.HasBrowser,
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

	fullMsg := callsession_domain.CallSessionControlMessage{
		Type:    string(WSEventCallSessionPresence),
		Payload: CallSessionPresencePayload{Users: users},
	}
	for _, s := range recipients {
		if s == nil {
			continue
		}
		canList := h.authorizer == nil || h.authorizer.HasWorkspacePermission(s.UserID(), workspaceID, "call_session", "list_members", false)
		msg := fullMsg
		if !canList {
			selfOnly := make([]CallSessionPresenceUser, 0, 1)
			for _, u := range users {
				if u.UserID == s.UserID() {
					selfOnly = append(selfOnly, u)
					break
				}
			}
			msg = callsession_domain.CallSessionControlMessage{
				Type:    string(WSEventCallSessionPresence),
				Payload: CallSessionPresencePayload{Users: selfOnly},
			}
		}
		if err := s.Notify(msg); err != nil {
			h.logger.Printf("[CallSessionWS] presence notify session=%s user=%s: %v", s.ID(), s.UserID(), err)
		}
		if canList && boardSnap != nil {
			_ = s.Notify(callsession_domain.CallSessionControlMessage{
				Type:    string(WSEventTelephonyBoard),
				Payload: boardSnap,
			})
		}
	}
}
