package calllist

import (
	"strings"
	"time"
)

type CallOutcome struct {
	Disposition string
	CallbackAt  *time.Time
}

func StampedCallOutcome(disposition, refusal string, callbackAt *time.Time) (CallOutcome, bool) {
	disposition = strings.TrimSpace(disposition)
	if disposition == "" || disposition == DispositionRefused {
		return CallOutcome{}, false
	}
	outcome := CallOutcome{Disposition: disposition}
	if disposition == DispositionCallback && strings.TrimSpace(refusal) == "" && callbackAt != nil {
		at := *callbackAt
		outcome.CallbackAt = &at
	}
	return outcome, true
}
