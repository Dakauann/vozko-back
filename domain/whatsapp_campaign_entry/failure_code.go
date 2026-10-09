package whatsapp_campaign_entry

import (
	"errors"

	"vozko/domain/balance"
)

const (
	ErrorCodeMonthlySendCapReached = 900009
	ErrorCodeSendWindowClosed      = 900010
)

func FailureCode(err error) int {
	switch {
	case errors.Is(err, balance.ErrMonthlySendCapReached):
		return ErrorCodeMonthlySendCapReached
	case errors.Is(err, balance.ErrSendWindowClosed):
		return ErrorCodeSendWindowClosed
	}
	return 0
}
