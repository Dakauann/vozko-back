package lead_usecase

import (
	"errors"
	"fmt"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/domain/workspace"
)

var errFilterChecksIncomplete = errors.New("lead filter checks: a required dependency is missing")

type FilterChecks struct {
	viewers viewers
}

func NewFilterChecks(permissions Permissions, definitions DefinitionSource) (*FilterChecks, error) {
	if permissions == nil || definitions == nil {
		return nil, errFilterChecksIncomplete
	}
	return &FilterChecks{viewers: viewers{permissions: permissions, definitions: definitions}}, nil
}

func (c *FilterChecks) CheckLeadFilter(a Actor, f crmfilter.Filter) error {
	if a.WorkspaceID == "" {
		return lead.ErrLeadWorkspaceRequired
	}
	if !c.viewers.allowed(a, workspace.ActionRead) {
		return lead.ErrLeadForbidden
	}
	v, err := c.viewers.of(a)
	if err != nil {
		return err
	}
	bound, err := bindLeadFilter(v, f)
	if err != nil {
		return err
	}
	if err := lead.ValidateFilter(bound); err != nil {
		return fmt.Errorf("%w: %w", lead.ErrLeadFilterInvalid, err)
	}
	return nil
}
