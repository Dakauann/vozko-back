package workflow

import (
	"encoding/json"
	"strings"
)

func RequestMap(value interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}

	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}

	if raw, ok := value.(string); ok {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			return parsed
		}
	}

	return nil
}
