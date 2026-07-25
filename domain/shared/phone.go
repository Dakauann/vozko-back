package shared

import "strings"

func EnsureDialablePhoneNumber(number string) string {
	trimmed := strings.TrimSpace(number)
	if trimmed == "" {
		return ""
	}

	digits := extractDigits(trimmed)
	if len(digits) == 12 && strings.HasPrefix(digits, "55") {
		ddd := digits[2:4]
		localPart := digits[4:]

		if isLandline(localPart) {
			return "0" + ddd + localPart
		}

		if shouldAddMobileNinthDigit(localPart) {
			return digits[:4] + "9" + localPart
		}
		return digits
	}

	if len(digits) == 13 && strings.HasPrefix(digits, "55") {

		if digits[2] == '0' {
			return digits[2:]
		}
		return digits
	}

	return trimmed
}

func isLandline(localPart string) bool {
	if len(localPart) != 8 {
		return false
	}
	firstDigit := localPart[0]
	return firstDigit >= '2' && firstDigit <= '5'
}

func shouldAddMobileNinthDigit(localPart string) bool {
	if len(localPart) != 8 {
		return false
	}

	firstDigit := localPart[0]
	return firstDigit >= '6' && firstDigit <= '9'
}

func extractDigits(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))

	for _, r := range value {
		if r >= '0' && r <= '9' {
			builder.WriteRune(r)
		}
	}

	return builder.String()
}
