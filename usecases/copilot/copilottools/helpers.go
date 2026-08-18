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

func trimLower(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// argStringList reads an array argument, tolerating the single string a model
// sometimes sends when it means a one-element list.
func argStringList(args map[string]interface{}, key string) []string {
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

// argToolBindings reads an array of {name, config} objects. Entries without a
// name are dropped here rather than forwarded, so the use case's tool
// validation reports on real requests only.
func argToolBindings(args map[string]interface{}, key string) []toolBindingArg {
	raw, ok := args[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]toolBindingArg, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		binding := toolBindingArg{Name: name}
		if cfg, ok := m["config"].(map[string]interface{}); ok && len(cfg) > 0 {
			binding.Config = cfg
		}
		out = append(out, binding)
	}
	return out
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
