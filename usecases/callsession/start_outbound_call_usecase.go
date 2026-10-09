package callsession_usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vozko/domain/callsession"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/shared"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type startOutboundCallUseCase struct {
	callSource conversation.CallSource
	admission  callsession.CallAdmissionCoordinator
	leads      callsession.LeadDialTargets
	callLists  callsession.CallListItems
}

func NewStartOutboundCallUseCase(
	callSource conversation.CallSource,
	admission callsession.CallAdmissionCoordinator,
	leads callsession.LeadDialTargets,
	callLists callsession.CallListItems,
) callsession.StartOutboundCallUseCase {
	return &startOutboundCallUseCase{
		callSource: callSource,
		admission:  admission,
		leads:      leads,
		callLists:  callLists,
	}
}

func (uc *startOutboundCallUseCase) Execute(ctx context.Context, input callsession.StartOutboundCallInput) (*callsession.StartOutboundCallResult, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" {
		return nil, callsession.ErrWorkspaceRequired
	}
	if strings.TrimSpace(input.UserID) == "" {
		return nil, callsession.ErrOwnerRequired
	}
	if uc.callSource == nil {
		return nil, callsession.ErrCallSourceNotConfigured
	}
	if uc.admission == nil {
		return nil, callsession.ErrAdmissionDependenciesMissing
	}

	target := strings.TrimSpace(input.TargetPhone)
	if target == "" {
		return nil, callsession.ErrTargetPhoneRequired
	}
	phoneNumber := shared.EnsureDialablePhoneNumber(target)
	trunkID := strings.TrimSpace(input.TrunkID)
	itemID := strings.TrimSpace(input.CallListItemID)

	leadID, err := uc.dialedLead(ctx, input, phoneNumber)
	if err != nil {
		return nil, err
	}

	callChannel := ""
	switch {
	case trunkID != "":
		callChannel = workspace_pricing.TelephonyChannelSIP
	case strings.TrimSpace(input.WhatsAppPhoneID) != "":
		callChannel = workspace_pricing.TelephonyChannelWhatsApp
	}
	lease, err := uc.admission.Acquire(ctx, callsession.CallAdmissionInput{
		WorkspaceID:      input.WorkspaceID,
		SlotPollInterval: 1 * time.Second,
		SlotPollTimeout:  30 * time.Second,
		ReservationTTL:   5 * time.Minute,
		OnWaitingForSlot: input.OnWaitingForSlot,
		CallChannel:      callChannel,
	})
	if err != nil {
		return nil, err
	}

	call, err := uc.callSource.Dial(ctx, conversation.CallDialInput{
		PhoneNumber:     phoneNumber,
		UserID:          input.UserID,
		WorkspaceID:     input.WorkspaceID,
		IsAdmin:         input.IsAdmin,
		WhatsAppPhoneID: input.WhatsAppPhoneID,
		TrunkID:         input.TrunkID,
	})
	if err != nil {
		_ = uc.admission.Release(lease)
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	return &callsession.StartOutboundCallResult{
		Call:                call,
		PhoneNumber:         phoneNumber,
		PerMinuteCostMicros: lease.PerMinuteCostMicros,
		ReservedMicros:      lease.ReservedMicros,
		Admission:           lease,
		LeadID:              leadID,
		TrunkID:             trunkID,
		CallListItemID:      itemID,
	}, nil
}

func (uc *startOutboundCallUseCase) dialedLead(ctx context.Context, input callsession.StartOutboundCallInput, phoneNumber string) (string, error) {
	if uc.leads == nil {
		return "", callsession.ErrLeadDialTargetsNotConfigured
	}
	leadID := strings.TrimSpace(input.LeadID)
	itemID := strings.TrimSpace(input.CallListItemID)
	if itemID != "" && leadID == "" {
		return "", callsession.ErrCallListItemNeedsLead
	}
	if leadID == "" {
		return uc.leads.IdentityLead(ctx, input.WorkspaceID, phoneNumber)
	}

	dial := callsession.LeadDial{WorkspaceID: input.WorkspaceID, LeadID: leadID, Number: phoneNumber, Purpose: lead.DialDirect}
	if itemID != "" {
		if uc.callLists == nil {
			return "", callsession.ErrCallListsNotConfigured
		}
		err := uc.callLists.CheckItemDial(ctx, callsession.CallListItemDial{
			WorkspaceID: input.WorkspaceID, UserID: input.UserID, ItemID: itemID, LeadID: leadID, Number: phoneNumber,
		})
		if err != nil {
			return "", err
		}
		dial.Purpose = lead.DialCallList
	}
	if err := uc.leads.CheckLead(ctx, dial); err != nil {
		return "", err
	}
	return leadID, nil
}
