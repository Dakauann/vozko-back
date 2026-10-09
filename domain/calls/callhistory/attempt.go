package callhistory

import "vozko/domain/calls/cdr"

const AttemptDirection = cdr.DirectionOutbound

func IsAttempt(c cdr.Call) bool {
	return c.Direction == AttemptDirection
}
