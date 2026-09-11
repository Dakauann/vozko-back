package audience_usecase

import (
	"context"

	ca "vozko/domain/audience"
)

// GetAlertChannelsUseCase answers what the alert form is allowed to offer.
//
// It reads the SAME directory ManageAlertRulesUseCase validates against, which
// is the whole point: the vocabulary endpoint used to serve a hardcoded list of
// both channels to every workspace, so the picker and the save could disagree,
// and they did. A workspace with no connected number was offered the unofficial
// channel, accepted, and then could not send.
type GetAlertChannelsUseCase struct {
	senders ca.AlertSenderDirectory
}

func NewGetAlertChannelsUseCase(senders ca.AlertSenderDirectory) *GetAlertChannelsUseCase {
	return &GetAlertChannelsUseCase{senders: senders}
}

// Execute reports every channel the product supports, each marked with whether
// this workspace can currently use it and which numbers it can send from.
//
// With no directory wired it reports the full vocabulary as available, matching
// what the endpoint did before: a deployment that cannot answer the question
// should degrade to the old behaviour, not to an empty picker.
func (uc *GetAlertChannelsUseCase) Execute(ctx context.Context, workspaceID string) ([]ca.AlertChannelStatus, error) {
	if uc.senders == nil {
		return []ca.AlertChannelStatus{
			{Channel: ca.AlertChannelOfficial, Available: true},
			{Channel: ca.AlertChannelUnofficial, Available: true},
		}, nil
	}
	statuses, err := uc.senders.ChannelStatus(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if statuses == nil {
		statuses = []ca.AlertChannelStatus{}
	}
	return statuses, nil
}
