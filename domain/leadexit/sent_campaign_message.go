package leadexit

import (
	"vozko/domain/buttonaction"
	"vozko/domain/conversation"
)

type SentCampaignMessage struct {
	MessageID  string
	CampaignID string
	Text       string
}

func CampaignMessageOf(m *conversation.Message) (SentCampaignMessage, bool) {
	if m == nil || m.MessageType == conversation.MessageTypeTemplate || m.SentBy.Kind() != conversation.SenderCampaign {
		return SentCampaignMessage{}, false
	}
	return SentCampaignMessage{MessageID: m.ID, CampaignID: m.SentBy.Participant(), Text: m.Text}, true
}

func (m SentCampaignMessage) ShownWords() []string {
	return buttonaction.ExitWordsShownIn(m.Text)
}
