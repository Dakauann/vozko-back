package tools

import "strings"

// This file is the SINGLE source of truth for the set of custom-tool parameter
// types the platform exposes and how each maps onto the JSON-Schema type the LLM
// function-calling API understands. Three callers share it so they can never
// drift:
//
//   - the AI services (infra/ai/openai, infra/ai/openrouter) call ResolveParamType
//     when building the LLM tool schema;
//   - the workflow builder validation (domain/workflow.ValidateAIAgentToolParams)
//     calls IsValidParamType so the builder rejects a bad type up front;
//   - the frontend custom-tool editor's type dropdown mirrors AllowedParamTypes.
//
// "What the builder accepts" and "what the runtime maps" are then the same set by
// construction — adding a type here updates both at once.

// paramTypeAlias is a platform semantic parameter type expressed as the underlying
// JSON-Schema type plus a human format hint appended to the parameter description
// so the model emits the right shape (e.g. an ISO date for "date").
type paramTypeAlias struct {
	Schema string
	Format string
}

// paramTypeAliases are the semantic types the custom-tool editor offers beyond the
// raw JSON-Schema primitives. Each resolves to "string" for the LLM, carrying its
// expected format as a hint.
var paramTypeAliases = map[string]paramTypeAlias{
	"date":     {Schema: "string", Format: "(formato: YYYY-MM-DD)"},
	"time":     {Schema: "string", Format: "(formato: HH:MM)"},
	"datetime": {Schema: "string", Format: "(formato: YYYY-MM-DDTHH:MM:SS-03:00, ISO 8601)"},
	"email":    {Schema: "string", Format: "(formato: email válido)"},
	"phone":    {Schema: "string", Format: "(formato: número com DDI, ex: +5511999999999)"},
	"enum":     {Schema: "string", Format: ""},
}

// baseJSONSchemaTypes are the raw JSON-Schema types the LLM accepts as-is, with no
// aliasing. A custom-tool parameter may declare one of these directly.
var baseJSONSchemaTypes = map[string]bool{
	"string":  true,
	"number":  true,
	"integer": true,
	"boolean": true,
	"array":   true,
	"object":  true,
}

// ResolveParamType maps a custom-tool parameter's declared type to the JSON-Schema
// type and a human format hint for the LLM tool schema. A semantic alias (date,
// email, …) becomes its underlying JSON-Schema type with a format hint; anything
// else passes through unchanged (already a JSON-Schema primitive, or a value the
// builder validation already rejected). Case-insensitive: the builder validates
// the same way, so a stored "Email" and a validated "email" stay consistent.
func ResolveParamType(raw string) (schemaType, formatHint string) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if a, ok := paramTypeAliases[key]; ok {
		return a.Schema, a.Format
	}
	return key, ""
}

// IsValidParamType reports whether a custom-tool parameter type is one the platform
// accepts: a semantic alias (date, time, datetime, email, phone, enum) or a base
// JSON-Schema type (string, number, integer, boolean, array, object). An empty
// type is allowed — it defaults to "string" at run time. Match is case-insensitive
// and trims surrounding space.
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

// AllowedParamTypes is the human-facing list of accepted custom-tool parameter
// types (JSON-Schema primitives first, then semantic aliases), for error messages
// and builder guidance.
func AllowedParamTypes() []string {
	return []string{
		"string", "number", "integer", "boolean", "array", "object",
		"date", "time", "datetime", "email", "phone", "enum",
	}
}
