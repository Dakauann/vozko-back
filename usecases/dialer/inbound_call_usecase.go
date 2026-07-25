package dialer_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/dialer"
	"vozko/domain/sip_trunk"
	"vozko/domain/voip"
)

const defaultInboundOfferTTL = 30 * time.Second
const defaultRetryWait = 30 * time.Second

type InboundCallUseCaseConfig struct {
	Sessions  dialer.DialerSessionRegistry
	Admission dialer.CallAdmissionCoordinator
	Executor  dialer.DialerInboundExecutor

	// Receptive, when set, is consulted before human routing: if a receptive
	// campaign is armed on the trunk it answers the call with an agent/workflow.
	Receptive dialer.ReceptiveInboundHandler

	Broker    *InboundOfferBroker
	OfferTTL  time.Duration
	RetryWait time.Duration
	Logger    *log.Logger
}

type InboundCallUseCase struct {
	sessions  dialer.DialerSessionRegistry
	admission dialer.CallAdmissionCoordinator
	executor  dialer.DialerInboundExecutor
	receptive dialer.ReceptiveInboundHandler
	broker    *InboundOfferBroker
	offerTTL  time.Duration
	retryWait time.Duration
	logger    *log.Logger

	mu sync.Mutex
}

func NewInboundCallUseCase(cfg InboundCallUseCaseConfig) (*InboundCallUseCase, error) {
	if cfg.Sessions == nil {
		return nil, fmt.Errorf("inbound call use case: sessions registry is required")
	}
	if cfg.Admission == nil {
		return nil, dialer.ErrAdmissionDependenciesMissing
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	offerTTL := cfg.OfferTTL
	if offerTTL <= 0 {
		offerTTL = defaultInboundOfferTTL
	}
	retryWait := cfg.RetryWait
	if retryWait <= 0 {
		retryWait = defaultRetryWait
	}
	broker := cfg.Broker
	if broker == nil {
		broker = NewInboundOfferBroker()
	}
	return &InboundCallUseCase{
		sessions:  cfg.Sessions,
		admission: cfg.Admission,
		executor:  cfg.Executor,
		receptive: cfg.Receptive,
		broker:    broker,
		offerTTL:  offerTTL,
		retryWait: retryWait,
		logger:    logger,
	}, nil
}

func (uc *InboundCallUseCase) SetExecutor(executor dialer.DialerInboundExecutor) {
	uc.mu.Lock()
	uc.executor = executor
	uc.mu.Unlock()
}

// SetReceptiveHandler wires the receptive (agent/workflow) inbound handler that
// is consulted before human routing.
func (uc *InboundCallUseCase) SetReceptiveHandler(h dialer.ReceptiveInboundHandler) {
	uc.mu.Lock()
	uc.receptive = h
	uc.mu.Unlock()
}

func (uc *InboundCallUseCase) HandleInboundInvite(ctx context.Context, invite sip_trunk.InboundInvite) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if invite.Dialog == nil {
		return errors.New("inbound invite missing dialog")
	}
	if err := uc.validateInvite(invite); err != nil {
		_ = invite.Dialog.Hangup(context.Background())
		return err
	}

	// Receptive routing takes precedence over human roulette: if a receptive
	// campaign is armed on this trunk, answer the call with its agent/workflow.
	// When handled, that path owns the dialog (and billing) end-to-end. It only
	// returns handled=false when no receptive campaign applies AND it has sent no
	// SIP signaling, so the human path below can run without double Trying/Ringing.
	uc.mu.Lock()
	receptive := uc.receptive
	uc.mu.Unlock()
	if receptive != nil {
		if handled, rerr := receptive.TryHandleInbound(ctx, invite); handled {
			return rerr
		} else if rerr != nil {
			uc.logger.Printf("inbound call: receptive handler error (falling back to human routing): %v", rerr)
		}
	}

	_ = invite.Dialog.Trying()
	_ = invite.Dialog.Ringing()

	lease, err := uc.admission.Acquire(ctx, dialer.CallAdmissionInput{
		WorkspaceID:      invite.WorkspaceID,
		SlotPollInterval: 1 * time.Second,
		SlotPollTimeout:  30 * time.Second,
		ReservationTTL:   5 * time.Minute,
	})
	if err != nil {
		_ = invite.Dialog.Hangup(context.Background())
		return err
	}

	leaseReleased := false
	defer func() {
		if !leaseReleased {
			_ = uc.admission.Release(lease)
		}
	}()

	offerID := uuid.NewString()

	target := uc.selectAndReserve(invite.WorkspaceID, offerID)
	if target == nil {
		const retryInterval = 2 * time.Second
		retryDeadline := time.Now().Add(uc.retryWait)
		for time.Now().Before(retryDeadline) {
			select {
			case <-ctx.Done():
				_ = invite.Dialog.Hangup(context.Background())
				return ctx.Err()
			case <-time.After(retryInterval):
			}
			target = uc.selectAndReserve(invite.WorkspaceID, offerID)
			if target != nil {
				break
			}
		}
		if target == nil {
			_ = invite.Dialog.Hangup(context.Background())
			return dialer.ErrInboundNoAvailableAgents
		}
	}

	// The target is now reserved for this offer (selectAndReserve claimed it
	// atomically), so no concurrent inbound/transfer flow can ring it while this
	// one does. Release on every exit until Attach consumes the reservation; the
	// defer also covers ctx cancel, the offer timeout, decline, answer/attach
	// failure, and panics. Release is token-scoped, so once Attach has promoted
	// reserved->active (reservationActive=false below) the deferred call is a
	// no-op and never disturbs the live call.
	reservationActive := true
	defer func() {
		if reservationActive {
			target.Release(offerID)
		}
	}()

	offer := dialer.InboundCallOffer{
		OfferID:     offerID,
		CallID:      invite.ID,
		WorkspaceID: invite.WorkspaceID,
		TrunkID:     invite.TrunkID,
		FromNumber:  strings.TrimSpace(invite.FromNumber),
		ToNumber:    strings.TrimSpace(invite.ToNumber),
		ExpiresAt:   time.Now().Add(uc.offerTTL),
	}
	state := &InboundOfferState{
		Offer:           offer,
		TargetUserID:    target.UserID(),
		TargetSessionID: target.ID(),
		Response:        make(chan InboundOfferResponse, 1),
	}
	removeOffer := uc.broker.Store(state)
	defer removeOffer()

	if err := target.Notify(dialer.DialerControlMessage{Type: dialer.DialerControlInboundCall, Payload: offer}); err != nil {
		_ = invite.Dialog.Hangup(context.Background())
		return fmt.Errorf("notify inbound call offer: %w", err)
	}

	timer := time.NewTimer(uc.offerTTL)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		_ = invite.Dialog.Hangup(context.Background())
		return ctx.Err()
	case <-timer.C:
		_ = invite.Dialog.Hangup(context.Background())
		return dialer.ErrInboundOfferNotFound
	case response := <-state.Response:
		if !response.Accepted {
			_ = invite.Dialog.Hangup(context.Background())
			return nil
		}
	}

	uc.mu.Lock()
	executor := uc.executor
	uc.mu.Unlock()
	if executor == nil {
		_ = invite.Dialog.Hangup(context.Background())
		return dialer.ErrInboundExecutorNotConfigured
	}

	callSession, err := invite.Dialog.Answer(ctx, &voip.RecordingMeta{
		WorkspaceID: invite.WorkspaceID,
	})
	if err != nil {
		_ = invite.Dialog.Hangup(context.Background())
		return fmt.Errorf("answer inbound call: %w", err)
	}
	if callSession.PhoneNumber == "" {
		callSession.PhoneNumber = offer.FromNumber
	}

	call, err := executor.AttachInboundCall(ctx, dialer.AttachInboundCallInput{
		OfferID:     offerID,
		WorkspaceID: invite.WorkspaceID,
		UserID:      target.UserID(),
		Session:     target,
		PhoneNumber: offer.FromNumber,
		SIPTrunkID:  invite.TrunkID,
		CallSession: callSession,
		Admission:   lease,
		StartedAt:   time.Now(),
	})
	if err != nil {
		_ = invite.Dialog.Hangup(context.Background())
		return fmt.Errorf("attach inbound call: %w", err)
	}

	// Attach consumed the reservation (reserved->active); disarm the deferred
	// release so the now-attached call is left untouched on return.
	reservationActive = false
	leaseReleased = true

	select {
	case <-ctx.Done():
		_ = call.Hangup()
		return ctx.Err()
	case <-call.Done():
		return nil
	}
}

func (uc *InboundCallUseCase) Accept(ctx context.Context, input dialer.AcceptInboundCallInput) error {
	return uc.broker.Accept(ctx, input)
}

func (uc *InboundCallUseCase) Decline(ctx context.Context, input dialer.DeclineInboundCallInput) error {
	return uc.broker.Decline(ctx, input)
}

func (uc *InboundCallUseCase) validateInvite(invite sip_trunk.InboundInvite) error {
	workspaceID := strings.TrimSpace(invite.WorkspaceID)
	if workspaceID == "" {
		return dialer.ErrWorkspaceRequired
	}
	if invite.Trunk == nil {
		return dialer.ErrInboundTrunkUnsupported
	}
	if err := invite.Trunk.CanAcceptInbound(workspaceID); err != nil {
		return dialer.ErrInboundTrunkUnsupported
	}
	return nil
}

// selectAndReserve picks the first available agent and atomically reserves it for
// this offer, removing it from the availability pool for any concurrent
// inbound/transfer flow the instant it is chosen. Reserve is a compare-and-set:
// a candidate that another flow claimed between ListAvailable and here simply
// fails to reserve and is skipped, so the same agent is never double-rung.
func (uc *InboundCallUseCase) selectAndReserve(workspaceID, offerID string) dialer.DialerSession {
	for _, session := range uc.sessions.ListAvailable(workspaceID) {
		if session == nil {
			continue
		}
		if session.Reserve(offerID) {
			return session
		}
	}
	return nil
}

var _ dialer.InboundCallUseCase = (*InboundCallUseCase)(nil)
