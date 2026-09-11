package audience

import (
	"context"
	"fmt"
)

// Channel readiness: whether this workspace can actually send on a channel,
// and from which number.
//
// Added 2026-09-10 after a production report. The alert vocabulary endpoint
// advertised BOTH channels to every workspace from a hardcoded list, and
// AlertRule.Validate only demanded a sender for the OFFICIAL channel. So the
// two channels failed in completely different ways:
//
//	official    no approved number  -> Validate refuses at save. Loud, correct.
//	unofficial  no connected number -> saves, shows "Regra ativa", and dies at
//	                                   dispatch into LastError, where nobody is
//	                                   looking.
//
// An alert that silently cannot fire is worse than no alert at all, because
// the operator stops watching manually once they believe they are covered.
// This closes that asymmetry: the same question is now asked of both channels,
// and it is asked of the workspace rather than of a constant.
//
// The same endpoint already sent its METRIC vocabulary dynamically, with the
// comment "Sent rather than hardcoded so the picker and the evaluator cannot
// disagree about what a metric means". The channel list did exactly the thing
// that design existed to prevent.

// AlertSender is one number a channel can send from: an unofficial instance or
// an official business phone.
type AlertSender struct {
	ID string `json:"id"`
	// Label is what the operator recognises, never an internal id.
	Label string `json:"label"`
}

// AlertChannelStatus is whether a workspace can use a channel right now.
type AlertChannelStatus struct {
	Channel   AlertChannel `json:"channel"`
	Available bool         `json:"available"`
	// Reason names why it is unavailable, as a stable key the client
	// translates. A greyed-out control with no explanation is what sends
	// people to support.
	Reason  string        `json:"reason,omitempty"`
	Senders []AlertSender `json:"senders"`
}

const (
	// AlertChannelReasonNoSender: the channel exists in this deployment, but
	// this workspace has no number connected to send from.
	AlertChannelReasonNoSender = "no_sender"
	// AlertChannelReasonNotEnabled: the channel is switched off for this
	// deployment entirely, so no workspace can use it.
	AlertChannelReasonNotEnabled = "not_enabled"
)

// AlertSenderDirectory answers "can this workspace send on this channel, and
// from where".
//
// A port, so the domain never learns what an unofficial instance or a business
// phone is. The composition root is the only place that knows both.
type AlertSenderDirectory interface {
	ChannelStatus(ctx context.Context, workspaceID string) ([]AlertChannelStatus, error)
}

// ChannelStatusFor finds one channel's status. The second return is false when
// the directory said nothing about it.
func ChannelStatusFor(list []AlertChannelStatus, channel AlertChannel) (AlertChannelStatus, bool) {
	for _, s := range list {
		if s.Channel == channel {
			return s, true
		}
	}
	return AlertChannelStatus{}, false
}

// ValidateSender checks a rule can actually be sent, given what the workspace
// currently has connected.
//
// Deliberately separate from Validate: that one checks the rule against
// itself and is pure, this one checks it against the world and needs the
// directory's answer passed in.
//
// An EMPTY list means the deployment could not be asked, and the check stands
// down rather than blocking every rule in the product. This is a guard against
// a rule that lies about being armed, not a new dependency for saving one.
func (r AlertRule) ValidateSender(statuses []AlertChannelStatus) error {
	if len(statuses) == 0 {
		return nil
	}

	// A disabled rule is a draft. It claims nothing, so it may sit on a channel
	// the workspace has not connected yet: writing the rule before scanning the
	// number is a reasonable order to work in.
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
		// One number is unambiguous, and AlertRule.InstanceID has always
		// documented empty as "whichever one this workspace has". With several,
		// that stops being an answer: resolving it at send time would pick for
		// the operator, silently, and possibly from the wrong number.
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

// senderID is the field that names the number, which differs per channel.
func (r AlertRule) senderID() string {
	if r.Channel == AlertChannelOfficial {
		return r.BusinessPhoneID
	}
	return r.InstanceID
}
