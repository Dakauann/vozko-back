package emailinbox

import (
	"errors"

	"vozko/domain/conversation"
	"vozko/domain/emailsending"
)

const (
	ProviderDetailAddressSuppressed    = "email_address_suppressed"
	ProviderDetailSenderNotProvisioned = "email_sender_not_provisioned"
	ProviderDetailSenderPaused         = "email_sender_paused"
)

func WindowOf(account *emailsending.SenderAccount, suppressed bool) conversation.WindowState {
	if account == nil {
		return conversation.ClosedByProvider(ProviderDetailSenderNotProvisioned)
	}
	if err := account.CanSendOnDemand(); err != nil {
		if errors.Is(err, emailsending.ErrAccountPaused) {
			return conversation.ClosedByProvider(ProviderDetailSenderPaused)
		}
		return conversation.ClosedByProvider(ProviderDetailSenderNotProvisioned)
	}
	if suppressed {
		return conversation.ClosedByProvider(ProviderDetailAddressSuppressed)
	}
	return conversation.OpenWindow(nil)
}
