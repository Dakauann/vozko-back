package campaign

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/agent"
)

var (
	ErrWorkflowNotFound      = errors.New("campaign: the workflow does not exist")
	ErrWorkflowForbidden     = errors.New("campaign: the workflow belongs to another workspace")
	ErrWorkflowVarsMissing   = errors.New("campaign: the workflow needs campaign variables that are missing from a contact's metadata")
	ErrAgentNotFound         = errors.New("campaign: the agent does not exist in this workspace")
	ErrAgentVarsMissing      = agent.ErrAgentRequiredVariableMissing
	ErrAutomationUnavailable = errors.New("campaign: the workflow and agent checks are not available")
)

type EntryMetadata struct {
	Label    string
	Metadata map[string]any
}

func EntryMetadataOf(leadID, number string, metadata map[string]any) EntryMetadata {
	label := "number " + number
	if leadID != "" {
		label = "lead " + leadID
	}
	return EntryMetadata{Label: label, Metadata: metadata}
}

func (a Automation) Configured() bool {
	return strings.TrimSpace(a.AgentID) != "" || strings.TrimSpace(a.WorkflowID) != ""
}

func RequireWorkflowVars(keys []string, entries []EntryMetadata) error {
	for _, entry := range entries {
		for _, key := range keys {
			value, exists := entry.Metadata[key]
			if !exists {
				return fmt.Errorf("%w: key %q missing for %s", ErrWorkflowVarsMissing, key, entry.Label)
			}
			if text, isText := value.(string); isText && strings.TrimSpace(text) == "" {
				return fmt.Errorf("%w: key %q is empty for %s", ErrWorkflowVarsMissing, key, entry.Label)
			}
		}
	}
	return nil
}

func RequireAgentVars(a *agent.Agent, entries []EntryMetadata) error {
	if a == nil {
		return ErrAgentNotFound
	}
	for _, entry := range entries {
		if err := agent.ValidateEntryMetadata(a, entry.Metadata); err != nil {
			return fmt.Errorf("%s: %w", entry.Label, err)
		}
	}
	return nil
}
