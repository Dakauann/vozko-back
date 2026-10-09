package address

import "strings"

var cityStateSeparators = []string{" - ", "/", ","}

func CityKeyFromText(raw, state string) (string, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", false
	}
	given, hasGiven := "", strings.TrimSpace(state) != ""
	if hasGiven {
		code, ok := StateCode(state)
		if !ok {
			return "", false
		}
		given = code
	}
	if strings.Contains(text, cityKeySeparator) {
		key, ok := ParseCityKey(text)
		if !ok || (hasGiven && !strings.HasPrefix(key, strings.ToLower(given)+cityKeySeparator)) {
			return "", false
		}
		return key, true
	}
	name, written := splitCityState(text)
	switch {
	case written == "" && !hasGiven:
		return "", false
	case written == "":
		written = given
	}
	code, ok := StateCode(written)
	if !ok || (hasGiven && code != given) {
		return "", false
	}
	key := CityKey(name, code)
	return key, key != ""
}

func splitCityState(text string) (string, string) {
	cut := -1
	width := 0
	for _, separator := range cityStateSeparators {
		if i := strings.LastIndex(text, separator); i > cut {
			cut, width = i, len(separator)
		}
	}
	if cut < 0 {
		return text, ""
	}
	return strings.TrimSpace(text[:cut]), strings.TrimSpace(text[cut+width:])
}
