package audience_usecase

import (
	"context"

	ca "vozko/domain/audience"
)

type GetAlertChannelsUseCase struct {
	senders ca.AlertSenderDirectory
}

func NewGetAlertChannelsUseCase(senders ca.AlertSenderDirectory) *GetAlertChannelsUseCase {
	return &GetAlertChannelsUseCase{senders: senders}
}

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
