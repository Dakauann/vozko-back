package cmcom

import (
	"errors"
	"fmt"
	"strings"
)

const (
	gatewayPrefix  = "00"
	minPhoneDigits = 8
	maxPhoneDigits = 15
	minRouteDigits = 3
	maxRouteDigits = 16
)

var ErrPhoneInvalid = errors.New("cmcom: the phone number is not an international number")

type digitRange struct {
	min int
	max int
}

var (
	phoneDigits = digitRange{min: minPhoneDigits, max: maxPhoneDigits}
	routeDigits = digitRange{min: minRouteDigits, max: maxRouteDigits}
)

func GatewayNumber(phone string) (string, error) {
	digits, err := withoutPlus(phone, phoneDigits)
	if err != nil {
		return "", err
	}
	return gatewayPrefix + digits, nil
}

func PhoneFromGateway(raw string) (string, error) {
	return fromGateway(raw, phoneDigits)
}

func routeNumber(raw string) (string, error) {
	return withoutPlus(raw, routeDigits)
}

func routeFromGateway(raw string) (string, error) {
	return fromGateway(raw, routeDigits)
}

func withoutPlus(raw string, r digitRange) (string, error) {
	digits := strings.TrimPrefix(strings.TrimSpace(raw), "+")
	if err := r.check(digits); err != nil {
		return "", fmt.Errorf("%w: %q", err, raw)
	}
	return digits, nil
}

func fromGateway(raw string, r digitRange) (string, error) {
	trimmed := strings.TrimSpace(raw)
	digits, cut := strings.CutPrefix(trimmed, "+")
	if !cut {
		digits = strings.TrimPrefix(trimmed, gatewayPrefix)
	}
	if err := r.check(digits); err != nil {
		return "", fmt.Errorf("%w: %q", err, raw)
	}
	return digits, nil
}

func (r digitRange) check(digits string) error {
	if len(digits) < r.min || len(digits) > r.max || digits[0] == '0' {
		return ErrPhoneInvalid
	}
	for _, d := range digits {
		if d < '0' || d > '9' {
			return ErrPhoneInvalid
		}
	}
	return nil
}

func agentFromGateway(raw string) string {
	return strings.TrimPrefix(strings.TrimSpace(raw), gatewayPrefix)
}

func GatewayAgent(agentID string) string {
	return gatewayPrefix + strings.TrimSpace(agentID)
}
