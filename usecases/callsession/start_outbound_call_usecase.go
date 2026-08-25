package callsession_usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vozko/domain/callsession"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type startOutboundCallUseCase struct {
	callSource conversation.CallSource
	history    conversation.HistoryProvider
	admission  callsession.CallAdmissionCoordinator
}

func NewStartOutboundCallUseCase(
	callSource conversation.CallSource,
	history conversation.HistoryProvider,
	admission callsession.CallAdmissionCoordinator,
) callsession.StartOutboundCallUseCase {
	return &startOutboundCallUseCase{
		callSource: callSource,
		history:    history,
		admission:  admission,
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

	phoneNumber, err := uc.resolveTargetPhone(input)
	if err != nil {
		return nil, err
	}

	callChannel := ""
	if strings.TrimSpace(input.WhatsAppPhoneID) != "" {
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
		EntryID:         input.EntryID,
		EntryType:       uc.resolveDialEntryType(input),
		UserID:          input.UserID,
		WorkspaceID:     input.WorkspaceID,
		IsAdmin:         input.IsAdmin,
		WhatsAppPhoneID: input.WhatsAppPhoneID,
	})
	if err != nil {
		_ = uc.admission.Release(lease)
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	result := &callsession.StartOutboundCallResult{
		Call:                call,
		PhoneNumber:         phoneNumber,
		PerMinuteCostMicros: lease.PerMinuteCostMicros,
		ReservedMicros:      lease.ReservedMicros,
		Admission:           lease,
	}
	return result, nil
}

func (uc *startOutboundCallUseCase) resolveDialEntryType(input callsession.StartOutboundCallInput) string {
	entryType := strings.ToLower(strings.TrimSpace(input.EntryType))
	if entryType != "" {
		return entryType
	}

	if strings.TrimSpace(input.WhatsAppPhoneID) != "" {
		return "whatsapp"
	}

	return ""
}

func (uc *startOutboundCallUseCase) resolveTargetPhone(input callsession.StartOutboundCallInput) (string, error) {
	if p := strings.TrimSpace(input.TargetPhone); p != "" {
		return shared.EnsureDialablePhoneNumber(p), nil
	}

	entryID := strings.TrimSpace(input.EntryID)
	entryType := strings.ToLower(strings.TrimSpace(input.EntryType))
	if entryID == "" || entryType == "" {
		return "", callsession.ErrEntryFieldsRequired
	}
	if uc.history == nil {
		return "", callsession.ErrHistoryProviderNotConfigured
	}

	_, leadNumber, _, _, _, _, err := uc.history.GetEntryInfo(entryID, entryType)
	if err != nil {
		return "", fmt.Errorf("%w: %v", callsession.ErrEntryNotFound, err)
	}
	leadNumber = strings.TrimSpace(leadNumber)
	if leadNumber == "" {
		return "", callsession.ErrNoPhoneForEntry
	}
	return shared.EnsureDialablePhoneNumber(leadNumber), nil
}
