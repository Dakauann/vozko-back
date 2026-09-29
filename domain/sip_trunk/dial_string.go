package sip_trunk

import "strings"

const maxDialStringLength = 32

var dialSeparators = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "")

func NormalizeDialString(raw string) (string, error) {
	dial := dialSeparators.Replace(strings.TrimSpace(raw))
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
