package sip_trunk

import "vozko/domain/shared"

const maxDialStringLength = 32

func NormalizeDialString(raw string) (string, error) {
	dial := shared.CompactPhone(raw)
	if dial == "" || len(dial) > maxDialStringLength {
		return "", ErrInvalidPhoneNumber
	}
	for i, r := range dial {
		switch {
		case r >= '0' && r <= '9', r == '*', r == '#':
		case r == '+' && i == 0:
		default:
			return "", ErrInvalidPhoneNumber
		}
	}
	if dial == "+" {
		return "", ErrInvalidPhoneNumber
	}
	return dial, nil
}
