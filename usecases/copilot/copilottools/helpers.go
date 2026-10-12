package copilottools

import "strings"

func ArgString(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func ArgInt(args map[string]interface{}, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func ArgBoolPtr(args map[string]interface{}, key string) *bool {
	if v, ok := args[key].(bool); ok {
		return &v
	}
	return nil
}

func TrimLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func ArgStringList(args map[string]interface{}, key string) []string {
	switch v := args[key].(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	case string:
		if s := strings.TrimSpace(v); s != "" {
			return []string{s}
		}
	}
	return nil
}
