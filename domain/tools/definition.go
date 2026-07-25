package tools

import (
	"context"
	"fmt"
	"strings"
)

var (
	ErrToolConfigNotSupported = fmt.Errorf("tool does not support configuration")
	ErrToolConfigRequired     = fmt.Errorf("required config parameter is missing")
)

func ErrToolConfigMissing(key string) error {
	return fmt.Errorf("required tool config '%s' is missing", key)
}

func ErrToolConfigUnknown(key string) error {
	return fmt.Errorf("unknown tool config parameter '%s'", key)
}

func ErrToolConfigInvalidType(expected string) error {
	return fmt.Errorf("invalid config type, expected %s", expected)
}

type ToolVisibility string
type ToolCategory string

const (
	VisibilityMessaging ToolVisibility = "messaging"
	// VisibilityPostConversation marks tools the agent may only use once a
	// conversation has ended (analysis / staging wrap-up).
	VisibilityPostConversation ToolVisibility = "post_conversation"
)

const (
	CategoryAgentUtility ToolCategory = "agent_utility"
	CategoryMessaging    ToolCategory = "messaging"
	CategoryInsurance    ToolCategory = "insurance"
	CategoryAgentAction  ToolCategory = "agent_action"
	CategoryPayment      ToolCategory = "payment"
)

type Definition struct {
	Name               string
	DisplayName        string
	Description        string
	DisplayDescription string
	Parameters         map[string]Parameter
	Required           []string
	Visibility         []ToolVisibility
	Category           ToolCategory
	ConfigSchema       map[string]ConfigParameter
	RequiredConfig     []string
	RequiresConfig     bool
	AdminOnly          bool
}

func (d Definition) IsVisibleIn(v ToolVisibility) bool {
	if len(d.Visibility) == 0 {
		return true
	}
	for _, vis := range d.Visibility {
		if vis == v {
			return true
		}
	}
	return false
}

func (d Definition) ValidateConfig(config map[string]interface{}) error {
	if d.RequiresConfig && len(config) == 0 {
		return fmt.Errorf("tool '%s' requires configuration but none was provided", d.Name)
	}

	if d.ConfigSchema == nil && len(config) > 0 {
		return ErrToolConfigNotSupported
	}

	for _, key := range d.RequiredConfig {
		val, ok := config[key]
		if !ok || val == nil {
			return ErrToolConfigMissing(key)
		}
		if str, ok := val.(string); ok && strings.TrimSpace(str) == "" {
			return ErrToolConfigMissing(key)
		}
	}

	for key, val := range config {
		schema, ok := d.ConfigSchema[key]
		if !ok {
			return ErrToolConfigUnknown(key)
		}
		if err := schema.Validate(val); err != nil {
			return err
		}
	}

	return nil
}

func (d Definition) WithConfigExpansion(config map[string]interface{}) Definition {
	result := d

	result.Parameters = make(map[string]Parameter, len(d.Parameters))
	for k, v := range d.Parameters {
		result.Parameters[k] = v
	}

	var extraDesc string

	if pathParams, ok := config["path_params"].([]interface{}); ok && len(pathParams) > 0 {
		params := parseSchemaParams(pathParams)
		if len(params) > 0 {
			result.Parameters["path_values"] = Parameter{
				Type:        "object",
				Description: buildParamDescription("Path parameters to replace in the URL", params),
			}
			extraDesc += buildSchemaSection("Path Parameters", params)
		}
	}

	if querySchema, ok := config["query_schema"].([]interface{}); ok && len(querySchema) > 0 {
		params := parseSchemaParams(querySchema)
		if len(params) > 0 {
			result.Parameters["query_values"] = Parameter{
				Type:        "object",
				Description: buildParamDescription("Query parameters to append to the URL", params),
			}
			extraDesc += buildSchemaSection("Query Parameters", params)
		}
	}

	if bodySchema, ok := config["body_schema"].([]interface{}); ok && len(bodySchema) > 0 {
		params := parseSchemaParams(bodySchema)
		if len(params) > 0 {
			result.Parameters["body_values"] = Parameter{
				Type:        "object",
				Description: buildParamDescription("JSON body fields to send", params),
			}
			extraDesc += buildSchemaSection("Body Parameters", params)
		}
	}

	if url, ok := config["url"].(string); ok && url != "" {
		method, _ := config["method"].(string)
		extraDesc = fmt.Sprintf("\n\nConfigured endpoint: %s %s%s", method, url, extraDesc)
	}

	if extraDesc != "" {
		result.Description = d.Description + extraDesc
	}

	return result
}

func parseSchemaParams(raw []interface{}) []SchemaParameter {
	params := make([]SchemaParameter, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		param := SchemaParameter{
			Name:        getString(m, "name"),
			Type:        getString(m, "type"),
			Description: getString(m, "description"),
			Required:    getBool(m, "required"),
			Example:     getString(m, "example"),
		}
		if props, ok := m["properties"].([]interface{}); ok && len(props) > 0 {
			param.Properties = parseSchemaParams(props)
		}
		if items, ok := m["items"].(map[string]interface{}); ok {
			param.Items = &SchemaParameter{
				Name:        getString(items, "name"),
				Type:        getString(items, "type"),
				Description: getString(items, "description"),
				Required:    getBool(items, "required"),
				Example:     getString(items, "example"),
			}
			if itemProps, ok := items["properties"].([]interface{}); ok && len(itemProps) > 0 {
				param.Items.Properties = parseSchemaParams(itemProps)
			}
		}
		if param.Name != "" {
			params = append(params, param)
		}
	}
	return params
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func buildParamDescription(base string, params []SchemaParameter) string {
	if len(params) == 0 {
		return base
	}
	desc := base + ". Fields: "
	for i, p := range params {
		if i > 0 {
			desc += ", "
		}
		req := ""
		if p.Required {
			req = " (required)"
		}
		desc += fmt.Sprintf("%s: %s%s", p.Name, p.Type, req)
	}
	return desc
}

func buildSchemaSection(title string, params []SchemaParameter) string {
	if len(params) == 0 {
		return ""
	}
	section := fmt.Sprintf("\n\n%s:", title)
	for _, p := range params {
		section += buildParamLine(p, 1)
	}
	return section
}

func buildParamLine(p SchemaParameter, indent int) string {
	indentStr := ""
	for i := 0; i < indent; i++ {
		indentStr += "  "
	}

	req := ""
	if p.Required {
		req = " [REQUIRED]"
	}
	example := ""
	if p.Example != "" {
		example = fmt.Sprintf(" (e.g., %s)", p.Example)
	}

	line := fmt.Sprintf("\n%s- %s (%s)%s: %s%s", indentStr, p.Name, p.Type, req, p.Description, example)

	if len(p.Properties) > 0 {
		for _, nested := range p.Properties {
			line += buildParamLine(nested, indent+1)
		}
	}

	if p.Items != nil {
		itemsDesc := fmt.Sprintf("\n%s  (array items: %s", indentStr, p.Items.Type)
		if p.Items.Description != "" {
			itemsDesc += " - " + p.Items.Description
		}
		itemsDesc += ")"
		line += itemsDesc
		if len(p.Items.Properties) > 0 {
			for _, nested := range p.Items.Properties {
				line += buildParamLine(nested, indent+1)
			}
		}
	}

	return line
}

type ConfigParameter struct {
	Type               string
	Description        string
	DisplayName        string
	DisplayDescription string
	Default            interface{}
	Options            []ConfigParameterOption
	Required           bool
}

type ConfigParameterOption struct {
	Value string
	Label string
}

func (cp ConfigParameter) Validate(val interface{}) error {
	if val == nil && cp.Required {
		return ErrToolConfigRequired
	}

	switch cp.Type {
	case "string":
		if _, ok := val.(string); !ok {
			return ErrToolConfigInvalidType("string")
		}
	case "number":
		switch val.(type) {
		case float64, int, int64:
		default:
			return ErrToolConfigInvalidType("number")
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return ErrToolConfigInvalidType("boolean")
		}
	case "object":
		if _, ok := val.(map[string]interface{}); !ok {
			return ErrToolConfigInvalidType("object")
		}
	case "array":
		if _, ok := val.([]interface{}); !ok {
			return ErrToolConfigInvalidType("array")
		}
	}

	return nil
}

type Parameter struct {
	Type               string
	Description        string
	DisplayName        string
	DisplayDescription string
	Items              *ParameterItems
	Enum               []string
}

type ParameterItems struct {
	Type        string
	Properties  map[string]Parameter
	Required    []string
	Description string
}

type SchemaParameter struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Required    bool              `json:"required"`
	Example     string            `json:"example,omitempty"`
	Properties  []SchemaParameter `json:"properties,omitempty"`
	Items       *SchemaParameter  `json:"items,omitempty"`
}

type ExecutionResult struct {
	Result             interface{}
	IsError            bool
	ContextUpdateText  string
	ShouldEndSession   bool
	DialStatusOverride string
}

type Handler interface {
	Definition() Definition
	Execute(ctx context.Context, params map[string]interface{}) (ExecutionResult, error)
	ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (ExecutionResult, error)
}

type ToolContext struct {
	WorkspaceID  string
	CampaignID   string
	CampaignType string
	Agent        interface{}
}

type ContextualHandler interface {
	Handler
	DefinitionWithContext(ctx ToolContext) Definition
}

type WorkspaceContextualHandler interface {
	Handler
	DefinitionForWorkspace(workspaceID string) Definition
}

type Service interface {
	Definitions() []Definition
	DefinitionsFor(visibility ToolVisibility) []Definition
	Execute(ctx context.Context, name string, params map[string]interface{}) (ExecutionResult, error)
	ExecuteWithConfig(ctx context.Context, name string, config map[string]interface{}, params map[string]interface{}) (ExecutionResult, error)
	Handler(name string) (Handler, bool)
}
