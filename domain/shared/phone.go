package shared

import "strings"

const brazilCountryCode = "55"

var phoneSeparators = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "")

var brazilAreaCodes = map[string]bool{
	"11": true, "12": true, "13": true, "14": true, "15": true, "16": true, "17": true, "18": true, "19": true,
	"21": true, "22": true, "24": true, "27": true, "28": true,
	"31": true, "32": true, "33": true, "34": true, "35": true, "37": true, "38": true,
	"41": true, "42": true, "43": true, "44": true, "45": true, "46": true, "47": true, "48": true, "49": true,
	"51": true, "53": true, "54": true, "55": true,
	"61": true, "62": true, "63": true, "64": true, "65": true, "66": true, "67": true, "68": true, "69": true,
	"71": true, "73": true, "74": true, "75": true, "77": true, "79": true,
	"81": true, "82": true, "83": true, "84": true, "85": true, "86": true, "87": true, "88": true, "89": true,
	"91": true, "92": true, "93": true, "94": true, "95": true, "96": true, "97": true, "98": true, "99": true,
}

var brazilNonGeographicSeries = []string{"0300", "0303", "0500", "0800", "0900"}

func CompactPhone(raw string) string {
	return phoneSeparators.Replace(strings.TrimSpace(raw))
}

func EnsureDialablePhoneNumber(number string) string {
	compact := CompactPhone(number)
	international := strings.HasPrefix(compact, "+")
	digits := strings.TrimPrefix(compact, "+")
	if digits == "" || !onlyDigits(digits) {
		return compact
	}
	if international && !strings.HasPrefix(digits, brazilCountryCode) {
		return compact
	}
	national, ok := brazilNationalNumber(digits, international)
	if !ok {
		return compact
	}
	areaCode, subscriber := national[:2], national[2:]
	if !brazilAreaCodes[areaCode] {
		return compact
	}
	return brazilCountryCode + areaCode + withMobileNinthDigit(subscriber)
}

func brazilNationalNumber(digits string, international bool) (string, bool) {
	switch {
	case international || (strings.HasPrefix(digits, brazilCountryCode) && (len(digits) == 12 || len(digits) == 13)):
		return nationalFromInternational(strings.TrimPrefix(digits, brazilCountryCode))
	case isNonGeographic(digits):
		return "", false
	case strings.HasPrefix(digits, "0") && (len(digits) == 11 || len(digits) == 12):
		return digits[1:], true
	case strings.HasPrefix(digits, "0") && (len(digits) == 13 || len(digits) == 14):
		return digits[3:], true
	case !strings.HasPrefix(digits, "0") && (len(digits) == 10 || len(digits) == 11):
		return digits, true
	}
	return "", false
}

func nationalFromInternational(rest string) (string, bool) {
	if strings.HasPrefix(rest, "0") {
		rest = rest[1:]
	}
	return rest, len(rest) == 10 || len(rest) == 11
}

func isNonGeographic(digits string) bool {
	for _, series := range brazilNonGeographicSeries {
		if strings.HasPrefix(digits, series) {
			return true
		}
	}
	return false
}

func withMobileNinthDigit(subscriber string) string {
	if len(subscriber) == 8 && subscriber[0] >= '7' && subscriber[0] <= '9' {
		return "9" + subscriber
	}
	return subscriber
}

func onlyDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
