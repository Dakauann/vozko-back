package conversation

import (
	"strings"
	"time"
)

const disclosureValidity = 24 * time.Hour

func WithAutomationDisclosure(disclosure, reply string, recentNewestFirst []*Message, now time.Time) string {
	disclosure = strings.TrimSpace(disclosure)
	if disclosure == "" || botSpokeRecently(recentNewestFirst, now) {
		return reply
	}
	return disclosure + "\n\n" + reply
}

func botSpokeRecently(recentNewestFirst []*Message, now time.Time) bool {
	for _, m := range recentNewestFirst {
		if m == nil {
			continue
		}
		switch m.MessageType {
		case MessageTypeOperator:
			return false
		case MessageTypeAIResponse:
			return now.Sub(m.CreatedAt) < disclosureValidity
		}
	}
	return false
}
