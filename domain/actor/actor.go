package actor

import "strings"

const (
	AIPrefix       = "ai:"
	WorkflowPrefix = "workflow:"
	SystemID       = "system"
)

type Kind string

const (
	KindHuman    Kind = "human"
	KindAI       Kind = "ai"
	KindWorkflow Kind = "workflow"
	KindSystem   Kind = "system"
)

func (k Kind) Valid() bool {
	switch k {
	case KindHuman, KindAI, KindWorkflow, KindSystem:
		return true
	}
	return false
}

func FormatAI(agentID string) string {
	return formatPrefixed(AIPrefix, agentID)
}

func IsAI(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), AIPrefix)
}

func ParseAI(id string) string {
	return parsePrefixed(AIPrefix, id)
}

// FormatWorkflow is the actor id of a workflow: workflow:<workflowID>.
func FormatWorkflow(workflowID string) string {
	return formatPrefixed(WorkflowPrefix, workflowID)
}

func IsWorkflow(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), WorkflowPrefix)
}

func ParseWorkflow(id string) string {
	return parsePrefixed(WorkflowPrefix, id)
}

// IsAutomation reports whether the actor is automation rather than a person or
// the system: an AI agent or a workflow.
func IsAutomation(id string) bool {
	return IsAI(id) || IsWorkflow(id)
}

func KindOf(actorID string) Kind {
	id := strings.TrimSpace(actorID)
	if id == "" || id == SystemID {
		return KindSystem
	}
	if IsAI(id) {
		return KindAI
	}
	if IsWorkflow(id) {
		return KindWorkflow
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
	if kind == KindWorkflow || IsWorkflow(id) {
		return KindWorkflow, FormatWorkflow(id)
	}
	if kind == KindSystem || id == SystemID || id == "" {
		return KindSystem, SystemID
	}
	return KindHuman, id
}

func formatPrefixed(prefix, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, prefix) {
		return id
	}
	return prefix + id
}

func parsePrefixed(prefix, id string) string {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(id, prefix))
}
