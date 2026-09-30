package sip_trunk_usecase

import (
	"context"
	"errors"
	"log"
	"time"

	cdr "vozko/domain/calls/cdr"
	"vozko/domain/callsession"
	"vozko/domain/sip_trunk"
	"vozko/domain/workflow"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	callsession_usecase "vozko/usecases/callsession"
)

const (
	inboundRingWindowDefault = 28 * time.Second
	inboundPerMemberDefault  = 15 * time.Second
	inboundChannel           = "sip"
)

var ErrNoMemberAvailable = errors.New("no member with SIP calling permission is available")

type CallLifecycle interface {
	Run(ctx context.Context, input callsession_usecase.OutboundCallLifecycleInput)
}

type InboundCallConfig struct {
	Sessions    callsession.CallSessionRegistry
	Admission   callsession.CallAdmissionCoordinator
	Ringer      *callsession_usecase.InboundRinger
	Executor    callsession.InboundCRMCallExecutor
	Permissions CallPermissions
	Engine      sip_trunk.Engine
	VoiceFlows  workflow.InboundVoiceFlows
	Lifecycle   CallLifecycle
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
	flow, err := uc.voiceFlow(invite)
	if err != nil {
		return err
	}
	if flow != nil {
		return uc.answerWithFlow(ctx, invite, flow)
	}
	return uc.ringMembers(ctx, invite)
}

func (uc *InboundCallUseCase) voiceFlow(invite sip_trunk.InboundInvite) (*workflow.Workflow, error) {
	if uc.cfg.VoiceFlows == nil {
		return nil, nil
	}
	return uc.cfg.VoiceFlows.FlowFor(invite.WorkspaceID, invite.TrunkID)
}

func (uc *InboundCallUseCase) admit(ctx context.Context, workspaceID string) (*callsession.CallAdmissionLease, error) {
	return uc.cfg.Admission.Acquire(ctx, callsession.CallAdmissionInput{
		WorkspaceID:      workspaceID,
		SlotPollInterval: time.Second,
		SlotPollTimeout:  5 * time.Second,
		ReservationTTL:   5 * time.Minute,
		CallChannel:      workspace_pricing.TelephonyChannelSIP,
	})
}

func (uc *InboundCallUseCase) answerWithFlow(ctx context.Context, invite sip_trunk.InboundInvite, flow *workflow.Workflow) error {
	if uc.cfg.Lifecycle == nil {
		return callsession.ErrBillingNotConfigured
	}
	lease, err := uc.admit(ctx, invite.WorkspaceID)
	if err != nil {
		return err
	}
	session, err := invite.Dialog.Answer(ctx)
	if err != nil {
		_ = uc.cfg.Admission.Release(lease)
		return err
	}
	call := newAnsweredTrunkCall(cdr.SIPInboundCallID(session.ID), invite.TrunkID, uc.cfg.Engine, session)
	billed := make(chan struct{})
	go func() {
		defer close(billed)
		uc.cfg.Lifecycle.Run(ctx, callsession_usecase.OutboundCallLifecycleInput{
			Call:        call,
			Admission:   lease,
			WorkspaceID: invite.WorkspaceID,
			StartedAt:   time.Now(),
			Direction:   cdr.DirectionInbound,
			PhoneTo:     invite.FromNumber,
		})
	}()
	call.Start()

	flowErr := uc.cfg.VoiceFlows.Answer(flow, workflow.InboundVoiceCall{
		WorkspaceID:  invite.WorkspaceID,
		TrunkID:      invite.TrunkID,
		CallID:       call.ID(),
		CallerNumber: invite.FromNumber,
		CalledNumber: invite.ToNumber,
		Call:         newLiveVoiceCall(call, session.Keys),
	})
	if flowErr != nil {
		uc.cfg.Logger.Printf("[SIPTrunk] voice workflow %s on call %s: %v", flow.ID, call.ID(), flowErr)
	}
	_ = call.Hangup()
	<-billed
	return nil
}

func (uc *InboundCallUseCase) ringMembers(ctx context.Context, invite sip_trunk.InboundInvite) error {
	candidates := uc.candidates(invite.WorkspaceID)
	if len(candidates) == 0 {
		return ErrNoMemberAvailable
	}
	lease, err := uc.admit(ctx, invite.WorkspaceID)
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
