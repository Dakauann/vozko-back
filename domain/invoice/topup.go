package invoice

import (
	"regexp"
	"strings"
)

const (
	MinTopUpBRL = 5.0
	MaxTopUpBRL = 50000.0
)

func ValidateTopUpAmount(amountBRL float64) error {
	if amountBRL < MinTopUpBRL {
		return ErrTopUpBelowMinimum
	}
	if amountBRL > MaxTopUpBRL {
		return ErrTopUpAboveMaximum
	}
	return nil
}

var clientKeyShape = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func ClientIdempotencyKey(workspaceID, clientKey string) (string, error) {
	key := strings.TrimSpace(clientKey)
	if key == "" {
		return "", nil
	}
	workspace := strings.TrimSpace(workspaceID)
	if workspace == "" || !clientKeyShape.MatchString(key) {
		return "", ErrInvalidIdempotencyKey
	}
	return "client:" + workspace + ":" + key, nil
}
