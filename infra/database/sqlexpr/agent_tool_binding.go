package sqlexpr

import "encoding/json"

func AgentBindsTool(alias, tool string) string {
	filter, _ := json.Marshal(map[string]interface{}{"bindings": []map[string]string{{"name": tool}}})
	return column(alias, "tool_bindings") + " @> CAST(" + Literal(string(filter)) + " AS jsonb)"
}
