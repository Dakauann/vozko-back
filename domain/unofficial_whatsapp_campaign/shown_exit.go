package unofficial_whatsapp_campaign

import (
	"errors"

	"vozko/domain/buttonaction"
	"vozko/domain/shared"
)

var ErrCampaignNeedsExit = errors.New("unofficial whatsapp campaign: every message variant must show the lead how to leave, with a command such as /sair")

func (m MessageSpec) RequireShownExit() error {
	shown := m.shownTexts()
	if len(shown) == 0 {
		return ErrCampaignNeedsExit
	}
	for _, text := range shown {
		if len(buttonaction.ExitWordsShownIn(text)) == 0 {
			return ErrCampaignNeedsExit
		}
	}
	return nil
}

func (m MessageSpec) shownTexts() []string {
	if m.Kind == KindAudio {
		return shared.NonEmptyTrimmed([]string{m.ExitText})
	}
	return m.Bodies
}

func (c *Campaign) RequireExitFor(edit *Campaign) error {
	if c.TargetsGroups() || c.Message.SameAs(edit.Message) {
		return nil
	}
	return edit.Message.RequireShownExit()
}
