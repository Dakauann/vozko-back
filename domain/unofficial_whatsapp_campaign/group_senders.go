package unofficial_whatsapp_campaign

import (
	"errors"
	"fmt"

	uwg "vozko/domain/unofficial_whatsapp_group"
)

var ErrGroupCampaignNoNumber = errors.New("none of the numbers this campaign may send through is usable")

type SenderNumber struct {
	InstanceID string `json:"instanceId"`
	Label      string `json:"label"`
}

type GroupSenders struct {
	JID     string          `json:"jid"`
	Name    string          `json:"name"`
	Senders []SenderNumber  `json:"senders"`
	Block   uwg.SenderBlock `json:"block,omitempty"`
}

func (g GroupSenders) Refusal() error {
	switch g.Block {
	case uwg.SenderNotInGroup:
		return fmt.Errorf("%w: none of your numbers is in the group %q", ErrCampaignTargetInvalid, g.label())
	case uwg.SenderCannotPost:
		return fmt.Errorf("%w: only admins post in %q and none of your numbers there is an admin", ErrCampaignTargetInvalid, g.label())
	}
	return nil
}

func (g GroupSenders) label() string {
	if g.Name != "" {
		return g.Name
	}
	return g.JID
}
