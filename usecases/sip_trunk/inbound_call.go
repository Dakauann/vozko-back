package sip_trunk_usecase

import (
	"context"
	"errors"
	"log"
	"time"

	cdr "vozko/domain/calls/cdr"
	"vozko/domain/callsession"
	"vozko/domain/sip_trunk"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	callsession_usecase "vozko/usecases/callsession"
)

const (
	inboundRingWindowDefault = 28 * time.Second
	inboundPerMemberDefault  = 15 * time.Second
	inboundChannel           = "sip"
)

var ErrNoMemberAvailable = errors.New("no member with SIP calling permission is available")

type InboundCallConfig struct {
	Sessions    callsession.CallSessionRegistry
	Admission   callsession.CallAdmissionCoordinator
	Ringer      *callsession_usecase.InboundRinger
	Executor    callsession.InboundCRMCallExecutor
	Permissions CallPermissions
	Engine      sip_trunk.Engine
	RingWindow  time.Duration
	PerMember   time.Duration
	Logger      *log.Logger
}

type InboundCallUseCase struct {
	cfg InboundCallConfig
}

var _ sip_trunk.InboundInviteHandler = (*InboundCallUseCase)(nil)

func NewInboundCallUseCase(cfg InboundCallConfig) *InboundCallUseCase {
	if cfg.RingWindow <= 0 {
		cfg.RingWindow = inboundRingWindowDefault
	}
	if cfg.PerMember <= 0 {
		cfg.PerMember = inboundPerMemberDefault
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	return &InboundCallUseCase{cfg: cfg}
}

func (uc *InboundCallUseCase) HandleInboundInvite(ctx context.Context, invite sip_trunk.InboundInvite) error {
	candidates := uc.candidates(invite.WorkspaceID)
	if len(candidates) == 0 {
		return ErrNoMemberAvailable
	}
	lease, err := uc.cfg.Admission.Acquire(ctx, callsession.CallAdmissionInput{
		WorkspaceID:      invite.WorkspaceID,
		SlotPollInterval: time.Second,
		SlotPollTimeout:  5 * time.Second,
		ReservationTTL:   5 * time.Minute,
		CallChannel:      workspace_pricing.TelephonyChannelSIP,
	})
	if err != nil {
		return err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			_ = uc.cfg.Admission.Release(lease)
		}
	}()

	_ = invite.Dialog.Ringing()
	outcome := uc.cfg.Ringer.Ring(ctx, callsession_usecase.RingRequest{
		Offer: callsession.InboundCallOffer{
			CallID:      invite.ID,
			WorkspaceID: invite.WorkspaceID,
			FromNumber:  invite.FromNumber,
			ToNumber:    invite.ToNumber,
			Channel:     inboundChannel,
		},
		Candidates:   candidates,
		PerCandidate: uc.cfg.PerMember,
		Deadline:     time.Now().Add(uc.cfg.RingWindow),
		CallerGone:   invite.Dialog.Done(),
	})
	if outcome.Session == nil {
		return nil
	}
	defer outcome.Session.Release(outcome.OfferID)

	session, err := invite.Dialog.Answer(ctx)
	if err != nil {
		return err
	}
	call := newAnsweredTrunkCall(cdr.SIPInboundCallID(session.ID), invite.TrunkID, uc.cfg.Engine, session)
	if err := uc.cfg.Executor.AttachInboundCRMCall(ctx, callsession.AttachInboundCRMCallInput{
		OfferID:     outcome.OfferID,
		WorkspaceID: invite.WorkspaceID,
		UserID:      outcome.Session.UserID(),
		Session:     outcome.Session,
		PhoneNumber: invite.FromNumber,
		Call:        call,
		Admission:   lease,
		StartedAt:   time.Now(),
	}); err != nil {
		_ = call.Hangup()
		return err
	}
	handedOff = true

	select {
	case <-call.Done():
	case <-ctx.Done():
		_ = call.Hangup()
		<-call.Done()
	}
	return nil
}

func (uc *InboundCallUseCase) candidates(workspaceID string) []callsession.CallSession {
	var eligible []callsession.CallSession
	for _, session := range uc.cfg.Sessions.ListAvailable(workspaceID) {
		if session == nil || session.WorkspaceID() != workspaceID || session.HasActiveCall() {
			continue
		}
		if uc.cfg.Permissions.MayCallThroughTrunks(session.UserID(), workspaceID, false) {
			eligible = append(eligible, session)
		}
	}
	return eligible
}
