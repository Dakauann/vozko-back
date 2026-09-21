package audience

import (
	"context"
	"fmt"
)

type AlertSender struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type AlertChannelStatus struct {
	Channel   AlertChannel  `json:"channel"`
	Available bool          `json:"available"`
	Reason    string        `json:"reason,omitempty"`
	Senders   []AlertSender `json:"senders"`
}

const (
	AlertChannelReasonNoSender   = "no_sender"
	AlertChannelReasonNotEnabled = "not_enabled"
)

type AlertSenderDirectory interface {
	ChannelStatus(ctx context.Context, workspaceID string) ([]AlertChannelStatus, error)
}

func ChannelStatusFor(list []AlertChannelStatus, channel AlertChannel) (AlertChannelStatus, bool) {
	for _, s := range list {
		if s.Channel == channel {
			return s, true
		}
	}
	return AlertChannelStatus{}, false
}

func (r AlertRule) ValidateSender(statuses []AlertChannelStatus) error {
	if len(statuses) == 0 {
		return nil
	}

	if !r.Enabled {
		return nil
	}

	status, known := ChannelStatusFor(statuses, r.Channel)
	if !known {
		return fmt.Errorf("%w: %s is not available in this workspace", ErrChannelUnavailable, r.Channel)
	}
	if !status.Available {
		if status.Reason == AlertChannelReasonNotEnabled {
			return fmt.Errorf("%w: %s is not enabled in this deployment", ErrChannelUnavailable, r.Channel)
		}
		return fmt.Errorf(
			"%w: %s has no connected number to send from, so this rule could never fire",
			ErrChannelUnavailable, r.Channel,
		)
	}
	if len(status.Senders) == 0 {
		return fmt.Errorf(
			"%w: %s has no connected number to send from, so this rule could never fire",
			ErrChannelUnavailable, r.Channel,
		)
	}

	named := r.senderID()
	if named == "" {
		if len(status.Senders) > 1 {
			return fmt.Errorf(
				"%w: this workspace has %d numbers on %s, so the rule has to name the one it sends from",
				ErrChannelUnavailable, len(status.Senders), r.Channel,
			)
		}
		return nil
	}

	for _, s := range status.Senders {
		if s.ID == named {
			return nil
		}
	}
	return fmt.Errorf(
		"%w: the number this rule sends from is no longer connected",
		ErrChannelUnavailable,
	)
}

func (r AlertRule) senderID() string {
	if r.Channel == AlertChannelOfficial {
		return r.BusinessPhoneID
	}
	return r.InstanceID
}
