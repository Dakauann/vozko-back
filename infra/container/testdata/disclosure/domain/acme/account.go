package acme

import "vozko/domain/conversation"

type Account struct{ AutomationDisclosure string }

func (a *Account) Automation() conversation.ChannelAutomation {
	return conversation.ChannelAutomation{EnableAgentResponses: true, Disclosure: a.AutomationDisclosure}
}
