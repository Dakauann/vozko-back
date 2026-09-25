package actor

import "strings"

func Split(id string) (string, Kind) {
	switch KindOf(id) {
	case KindAI:
		return ParseAI(id), KindAI
	case KindWorkflow:
		return ParseWorkflow(id), KindWorkflow
	case KindSystem:
		if strings.TrimSpace(id) == SystemID {
			return "", KindSystem
		}
	}
	return id, KindHuman
}

func Join(id string, kind Kind) string {
	if kind == KindSystem {
		return SystemID
	}
	if id == "" {
		return ""
	}
	switch kind {
	case KindAI:
		return FormatAI(id)
	case KindWorkflow:
		return FormatWorkflow(id)
	}
	return id
}
