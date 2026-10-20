package conversation

import "strings"

const workflowSourceLabel = "fluxo"

type SourceLabels struct {
	names map[string]string
}

func NewSourceLabels(actorNames map[string]string) SourceLabels {
	return SourceLabels{names: actorNames}
}

func (m *Message) sentByWorkflow() bool {
	return m != nil && m.SentBy.Kind() == SenderWorkflow && m.SentBy.Participant() != ""
}

func WorkflowSenders(history []*Message) []string {
	var actorIDs []string
	seen := map[string]bool{}
	for _, m := range history {
		if !m.sentByWorkflow() || seen[m.SentBy.ID()] {
			continue
		}
		seen[m.SentBy.ID()] = true
		actorIDs = append(actorIDs, m.SentBy.ID())
	}
	return actorIDs
}

func (l SourceLabels) Label(m *Message, content string) string {
	if !m.sentByWorkflow() || content == "" {
		return content
	}
	if name := strings.TrimSpace(l.names[m.SentBy.ID()]); name != "" {
		return "[" + workflowSourceLabel + " " + name + "] " + content
	}
	return "[" + workflowSourceLabel + "] " + content
}
