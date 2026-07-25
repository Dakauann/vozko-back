package node_executors

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"vozko/domain/workflow"
)

func buildStateContext(state *workflow.RunState) string {
	if state == nil || len(state.Vars) == 0 {
		return ""
	}

	var lastVars []string
	var userVars []string

	keys := make([]string, 0, len(state.Vars))
	for k := range state.Vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := state.Vars[k]

		if strings.HasPrefix(k, "_wait_") || strings.HasPrefix(k, "_node_") || strings.HasPrefix(k, "_ai_") {
			continue
		}

		formatted := formatStateValue(v)
		if len(formatted) > 500 {
			formatted = formatted[:500] + "..."
		}

		if strings.HasPrefix(k, "_last_") {
			displayKey := strings.TrimPrefix(k, "_last_")
			lastVars = append(lastVars, fmt.Sprintf("  %s: %s", displayKey, formatted))
		} else {
			userVars = append(userVars, fmt.Sprintf("  %s: %s", k, formatted))
		}
	}

	var sections []string
	if len(lastVars) > 0 {
		sections = append(sections, "Previous step output:\n"+strings.Join(lastVars, "\n"))
	}
	if len(userVars) > 0 {
		sections = append(sections, "Variables:\n"+strings.Join(userVars, "\n"))
	}

	return strings.Join(sections, "\n\n")
}

func formatStateValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]interface{}, []interface{}:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	default:
		return fmt.Sprintf("%v", val)
	}
}
