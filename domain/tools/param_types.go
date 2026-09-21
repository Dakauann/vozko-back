package tools

import "strings"

type paramTypeAlias struct {
	Schema string
	Format string
}

var paramTypeAliases = map[string]paramTypeAlias{
	"date":     {Schema: "string", Format: "(formato: YYYY-MM-DD)"},
	"time":     {Schema: "string", Format: "(formato: HH:MM)"},
	"datetime": {Schema: "string", Format: "(formato: YYYY-MM-DDTHH:MM:SS-03:00, ISO 8601)"},
	"email":    {Schema: "string", Format: "(formato: email válido)"},
	"phone":    {Schema: "string", Format: "(formato: número com DDI, ex: +5511999999999)"},
	"enum":     {Schema: "string", Format: ""},
}

var baseJSONSchemaTypes = map[string]bool{
	"string":  true,
	"number":  true,
	"integer": true,
	"boolean": true,
	"array":   true,
	"object":  true,
}

func ResolveParamType(raw string) (schemaType, formatHint string) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if a, ok := paramTypeAliases[key]; ok {
		return a.Schema, a.Format
	}
	return key, ""
}

func IsValidParamType(raw string) bool {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		return true
	}
	if baseJSONSchemaTypes[key] {
		return true
	}
	_, ok := paramTypeAliases[key]
	return ok
}

func AllowedParamTypes() []string {
	return []string{
		"string", "number", "integer", "boolean", "array", "object",
		"date", "time", "datetime", "email", "phone", "enum",
	}
}
