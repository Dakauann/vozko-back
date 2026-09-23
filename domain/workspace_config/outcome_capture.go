package workspace_config

import (
	"time"

	"vozko/domain/conversation"
)

func MergeOutcomeCapture(
	existing *conversation.OutcomeCapture,
	incoming *conversation.OutcomeCapture,
	now time.Time,
) (*conversation.OutcomeCapture, error) {
	if incoming == nil {
		return existing, nil
	}

	merged := *incoming
	merged.Normalize()
	if err := merged.Validate(); err != nil {
		return nil, err
	}

	switch {
	case !merged.Enabled:
		merged.EnabledAt = nil
	case existing != nil && existing.Enabled && existing.EnabledAt != nil:
		merged.EnabledAt = existing.EnabledAt
	default:
		stamped := now.UTC()
		merged.EnabledAt = &stamped
	}
	return &merged, nil
}
