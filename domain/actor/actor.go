package actor

import "strings"

const (
	AIPrefix = "ai:"
	SystemID = "system"
)

type Kind string

const (
	KindHuman  Kind = "human"
	KindAI     Kind = "ai"
	KindSystem Kind = "system"
)

func (k Kind) Valid() bool {
	switch k {
	case KindHuman, KindAI, KindSystem:
		return true
	}
	return false
}

func FormatAI(agentID string) string {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, AIPrefix) {
		return id
	}
	return AIPrefix + id
}

func IsAI(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), AIPrefix)
}

func ParseAI(id string) string {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, AIPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(id, AIPrefix))
}

func KindOf(actorID string) Kind {
	id := strings.TrimSpace(actorID)
	if id == "" || id == SystemID {
		return KindSystem
	}
	if IsAI(id) {
		return KindAI
	}
	return KindHuman
}

func Normalize(kind Kind, actorID string) (Kind, string) {
	id := strings.TrimSpace(actorID)
	if kind == KindAI || IsAI(id) {
		if IsAI(id) {
			return KindAI, FormatAI(ParseAI(id))
		}
		return KindAI, FormatAI(id)
	}
	if kind == KindSystem || id == SystemID || id == "" {
		return KindSystem, SystemID
	}
	return KindHuman, id
}
