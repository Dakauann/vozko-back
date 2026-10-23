package carriercampaign

import (
	"errors"
	"strings"

	"vozko/domain/campaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/customfield"
	"vozko/domain/rcs"
	"vozko/domain/shared"
	"vozko/domain/sms"
)

var ErrVariantChannelMismatch = errors.New("carriercampaign: an rcs variant carries rich content and an sms variant carries text, never both")

type Variant struct {
	Name     string                     `json:"name"`
	Content  rcs.Content                `json:"content,omitzero"`
	Text     string                     `json:"text,omitempty"`
	Bindings []campaign.VariableBinding `json:"bindings,omitempty"`
}

func (v Variant) hasContent() bool {
	return v.Content.Text != "" || v.Content.Media != nil || len(v.Content.Cards) > 0 || len(v.Content.Suggestions) > 0
}

func (v Variant) Validate(channel carriermessaging.Channel, fields []*customfield.Definition) error {
	switch channel {
	case carriermessaging.ChannelRCS:
		if v.Text != "" {
			return ErrVariantChannelMismatch
		}
		if err := v.Content.Validate(); err != nil {
			return err
		}
	case carriermessaging.ChannelSMS:
		if v.hasContent() {
			return ErrVariantChannelMismatch
		}
		if err := sms.Validate(v.Text); err != nil {
			return err
		}
	default:
		return ErrChannelInvalid
	}
	_, err := v.PlanBindings(channel, fields)
	return err
}

func (v Variant) Slots(channel carriermessaging.Channel) int {
	if channel == carriermessaging.ChannelSMS {
		return shared.HighestPositionalParameter([]string{v.Text})
	}
	return v.Content.VariableSlots()
}

func (v Variant) PlanBindings(channel carriermessaging.Channel, fields []*customfield.Definition) (campaign.BindingPlan, error) {
	if !channel.Valid() {
		return campaign.BindingPlan{}, ErrChannelInvalid
	}
	return campaign.PlanBindings(v.Bindings, v.Slots(channel), fields)
}

func blank(s string) bool {
	return strings.TrimSpace(s) == ""
}
