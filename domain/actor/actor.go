// Package actor provides a neutral identity model for humans, AI agents, and
// system actors used across CRM timeline, assignment history, and attendance.
// It reuses the dialer "ai:" owner prefix convention without importing dialer
// (avoids import cycles).
package actor

import "strings"

const (
	// AIPrefix namespaces AI attendant IDs so they never collide with user UUIDs.
	AIPrefix = "ai:"
	// SystemID is the actor_id used for automated platform actions.
	SystemID = "system"
)

// Kind distinguishes who performed an action for metrics and timelines.
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

// FormatAI returns the canonical attendant id for an AI agent (ai:{agentID}).
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

// IsAI reports whether id is an AI attendant identity.
func IsAI(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), AIPrefix)
}

// ParseAI returns the bare agent UUID when id is ai:{uuid}, otherwise "".
func ParseAI(id string) string {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, AIPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(id, AIPrefix))
}

// KindOf infers kind from a stored actor_id.
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

// Normalize returns kind + canonical actor_id for storage.
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
