package agent

import (
	"fmt"
	"strings"
)

func MetadataValueMissing(metadata map[string]any, key string) bool {
	value, exists := metadata[key]
	if !exists {
		return true
	}
	text, isText := value.(string)
	return isText && strings.TrimSpace(text) == ""
}

func MissingEntryMetadata(a *Agent, metadata map[string]any) []string {
	if a == nil {
		return nil
	}
	var missing []string
	for _, info := range a.RequiredVariableInfos() {
		if info.Required && MetadataValueMissing(metadata, info.Name) {
			missing = append(missing, info.Name)
		}
	}
	return missing
}

func ValidateEntryMetadata(a *Agent, metadata map[string]interface{}) error {
	if missing := MissingEntryMetadata(a, metadata); len(missing) > 0 {
		return fmt.Errorf("%w: variable %q is missing or empty", ErrAgentRequiredVariableMissing, missing[0])
	}
	return nil
}
