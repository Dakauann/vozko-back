package quiet

import "vozko/domain/conversation"

type Account struct{}

func (a *Account) Automation() conversation.ChannelAutomation {
	return conversation.ChannelAutomation{EnableAgentResponses: true}
}
