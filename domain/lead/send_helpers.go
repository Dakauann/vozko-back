package lead

import "strings"

func (s OptOutSource) OrDefault() OptOutSource {
	trimmed := strings.TrimSpace(string(s))
	if trimmed == "" {
		return OptOutOperator
	}
	return OptOutSource(trimmed)
}

func IDsOf(leads map[string]*Lead) []string {
	ids := make([]string, 0, len(leads))
	for _, l := range leads {
		if l != nil {
			ids = append(ids, l.ID)
		}
	}
	return ids
}
