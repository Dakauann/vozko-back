package copilottools

import "strings"

func argString(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func argInt(args map[string]interface{}, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func argBoolPtr(args map[string]interface{}, key string) *bool {
	if v, ok := args[key].(bool); ok {
		return &v
	}
	return nil
}

func inDeptScope(scope []string, deptID string) bool {
	if scope == nil {
		return true
	}
	for _, id := range scope {
		if id == deptID {
			return true
		}
	}
	return false
}
