package agent

import (
	"fmt"
	"strings"
)

func ValidateEntryMetadata(a *Agent, metadata map[string]interface{}) error {
	if a == nil {
		return nil
	}
	infos := a.RequiredVariableInfos()
	for _, info := range infos {
		if !info.Required {
			continue
		}
		val, exists := metadata[info.Name]
		if !exists {
			return fmt.Errorf("%w: variable %q", ErrAgentRequiredVariableMissing, info.Name)
		}
		if s, ok := val.(string); ok && strings.TrimSpace(s) == "" {
			return fmt.Errorf("%w: variable %q is empty", ErrAgentRequiredVariableMissing, info.Name)
		}
	}
	return nil
}
