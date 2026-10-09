package address

import "strings"

const cityKeySeparator = ":"

func CityNameKey(name string) string {
	return DistrictKey(name)
}

func CityKey(city, state string) string {
	name := CityNameKey(city)
	uf := strings.ToLower(strings.TrimSpace(state))
	if name == "" || uf == "" {
		return ""
	}
	return uf + cityKeySeparator + name
}

func ParseCityKey(raw string) (string, bool) {
	uf, name, found := strings.Cut(raw, cityKeySeparator)
	uf = strings.ToUpper(strings.TrimSpace(uf))
	if !found || !brazilStates[uf] {
		return "", false
	}
	key := CityKey(name, uf)
	return key, key != ""
}
