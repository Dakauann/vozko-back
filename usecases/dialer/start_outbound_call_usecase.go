package dialer_usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/dialer"
	"vozko/domain/shared"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type startOutboundCallUseCase struct {
	callSource conversation.CallSource
	history    conversation.HistoryProvider
	admission  dialer.CallAdmissionCoordinator
}

func NewStartOutboundCallUseCase(
	callSource conversation.CallSource,
	history conversation.HistoryProvider,
	admission dialer.CallAdmissionCoordinator,
) dialer.StartOutboundCallUseCase {
	return &startOutboundCallUseCase{
		callSource: callSource,
		history:    history,
		admission:  admission,
	}
}

func (uc *startOutboundCallUseCase) Execute(ctx context.Context, input dialer.StartOutboundCallInput) (*dialer.StartOutboundCallResult, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" {
		return nil, dialer.ErrWorkspaceRequired
	}
	if strings.TrimSpace(input.UserID) == "" {
		return nil, dialer.ErrOwnerRequired
	}
	if uc.callSource == nil {
		return nil, dialer.ErrCallSourceNotConfigured
	}
	if uc.admission == nil {
		return nil, dialer.ErrAdmissionDependenciesMissing
	}

	phoneNumber, err := uc.resolveTargetPhone(input)
	if err != nil {
		return nil, err
	}

	callChannel := ""
	if strings.TrimSpace(input.WhatsAppPhoneID) != "" {
		callChannel = workspace_pricing.TelephonyChannelWhatsApp
	}
	lease, err := uc.admission.Acquire(ctx, dialer.CallAdmissionInput{
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
		SIPTrunkID:      input.SIPTrunkID,
		WhatsAppPhoneID: input.WhatsAppPhoneID,
	})
	if err != nil {
		_ = uc.admission.Release(lease)
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	result := &dialer.StartOutboundCallResult{
		Call:                call,
		PhoneNumber:         phoneNumber,
		PerMinuteCostMicros: lease.PerMinuteCostMicros,
		ReservedMicros:      lease.ReservedMicros,
		Admission:           lease,
	}
	return result, nil
}

func (uc *startOutboundCallUseCase) resolveDialEntryType(input dialer.StartOutboundCallInput) string {
	entryType := strings.ToLower(strings.TrimSpace(input.EntryType))
	if entryType != "" {
		return entryType
	}

	if strings.TrimSpace(input.WhatsAppPhoneID) != "" {
		return "whatsapp"
	}

	if strings.TrimSpace(input.TargetPhone) != "" {
		return "sip"
	}

	return ""
}

func (uc *startOutboundCallUseCase) resolveTargetPhone(input dialer.StartOutboundCallInput) (string, error) {
	if p := strings.TrimSpace(input.TargetPhone); p != "" {
		return shared.EnsureDialablePhoneNumber(p), nil
	}

	entryID := strings.TrimSpace(input.EntryID)
	entryType := strings.ToLower(strings.TrimSpace(input.EntryType))
	if entryID == "" || entryType == "" {
		return "", dialer.ErrEntryFieldsRequired
	}
	if uc.history == nil {
		return "", dialer.ErrHistoryProviderNotConfigured
	}

	_, leadNumber, _, _, _, _, err := uc.history.GetEntryInfo(entryID, entryType)
	if err != nil {
		return "", fmt.Errorf("%w: %v", dialer.ErrEntryNotFound, err)
	}
	leadNumber = strings.TrimSpace(leadNumber)
	if leadNumber == "" {
		return "", dialer.ErrNoPhoneForEntry
	}
	return shared.EnsureDialablePhoneNumber(leadNumber), nil
}
