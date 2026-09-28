package whatsapp_campaign_entry

import (
	"errors"

	"vozko/domain/balance"
)

const ErrorCodeMonthlySendCapReached = 900009

func FailureCode(err error) int {
	if errors.Is(err, balance.ErrMonthlySendCapReached) {
		return ErrorCodeMonthlySendCapReached
	}
	return 0
}
