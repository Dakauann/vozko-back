package tools

import (
	"fmt"
	"strings"
)

var schemaKeywords = map[string]bool{"type": true, "description": true, "enum": true, "items": true, "properties": true, "required": true}

func (d Definition) JSONSchema() map[string]interface{} {
	schema := map[string]interface{}{"type": "object"}
	if props := propertiesSchema(d.Parameters); len(props) > 0 {
		schema["properties"] = props
	}
	if len(d.Required) > 0 {
		schema["required"] = d.Required
	}
	return schema
}

func propertiesSchema(params map[string]Parameter) map[string]interface{} {
	props := make(map[string]interface{}, len(params))
	for key, p := range params {
		if key = strings.TrimSpace(key); key != "" {
			props[key] = parameterSchema(p)
		}
	}
	return props
}

func parameterSchema(p Parameter) map[string]interface{} {
	schemaType, hint := ResolveParamType(p.Type)
	schema := map[string]interface{}{"type": schemaType, "description": withFormatHint(p.Description, hint)}
	if len(p.Enum) > 0 {
		schema["enum"] = p.Enum
	}
	if p.Items == nil {
		return schema
	}
	switch schemaType {
	case "array":
		schema["items"] = itemsSchema(*p.Items)
	case "object":
		objectProperties(schema, *p.Items)
	}
	return schema
}

func itemsSchema(items ParameterItems) map[string]interface{} {
	schemaType, hint := ResolveParamType(items.Type)
	schema := map[string]interface{}{"type": schemaType}
	if description := withFormatHint(items.Description, hint); description != "" {
		schema["description"] = description
	}
	if len(items.Enum) > 0 {
		schema["enum"] = items.Enum
	}
	switch schemaType {
	case "array":
		if items.Items != nil {
			schema["items"] = itemsSchema(*items.Items)
		}
	case "object":
		objectProperties(schema, items)
	}
	return schema
}

func objectProperties(schema map[string]interface{}, items ParameterItems) {
	props := propertiesSchema(items.Properties)
	if len(props) == 0 {
		return
	}
	schema["properties"] = props
	if len(items.Required) > 0 {
		schema["required"] = items.Required
	}
}

func withFormatHint(description, hint string) string {
	if hint == "" {
		return description
	}
	if description == "" {
		return hint
	}
	return description + " " + hint
}

func SchemaIssues(schema map[string]interface{}) []string {
	if schema["type"] != "object" {
		return []string{"the root must be an object"}
	}
	var issues []string
	schemaNodeIssues("$", schema, &issues)
	return issues
}

func schemaNodeIssues(path string, node map[string]interface{}, issues *[]string) {
	report := func(format string, args ...interface{}) {
		*issues = append(*issues, path+": "+fmt.Sprintf(format, args...))
	}
	for key := range node {
		if !schemaKeywords[key] {
			report("unknown keyword %q", key)
		}
	}
	schemaType, _ := node["type"].(string)
	if !baseJSONSchemaTypes[schemaType] {
		report("type %v is not a JSON Schema type", node["type"])
	}
	if description, ok := node["description"]; ok {
		if _, isText := description.(string); !isText {
			report("description is not text")
		}
	}
	if enum, ok := node["enum"]; ok {
		values, isList := stringList(enum)
		switch {
		case schemaType != "string":
			report("enum on a %s", schemaType)
		case !isList || len(values) == 0:
			report("enum must be a non empty list of strings")
		case repeated(values) != "":
			report("enum repeats %q", repeated(values))
		}
	}
	items, hasItems := node["items"]
	switch {
	case schemaType == "array" && !hasItems:
		report("array without items")
	case schemaType != "array" && hasItems:
		report("items on a %s", schemaType)
	case hasItems:
		if child, ok := items.(map[string]interface{}); ok {
			schemaNodeIssues(path+"[]", child, issues)
		} else {
			report("items is not a schema")
		}
	}
	properties, hasProperties := node["properties"]
	if hasProperties && schemaType != "object" {
		report("properties on a %s", schemaType)
	}
	declared, _ := properties.(map[string]interface{})
	if hasProperties && declared == nil {
		report("properties is not an object")
	}
	for name, raw := range declared {
		child, ok := raw.(map[string]interface{})
		switch {
		case strings.TrimSpace(name) == "":
			report("blank property name")
		case !ok:
			report("property %s is not a schema", name)
		default:
			schemaNodeIssues(path+"."+name, child, issues)
		}
	}
	if required, ok := node["required"]; ok {
		names, isList := stringList(required)
		if !isList {
			report("required must be a list of strings")
		}
		for _, name := range names {
			if _, ok := declared[name]; !ok {
				report("required %q is not a property", name)
			}
		}
		if name := repeated(names); name != "" {
			report("required repeats %q", name)
		}
	}
}

func stringList(raw interface{}) ([]string, bool) {
	switch values := raw.(type) {
	case []string:
		return values, true
	case []interface{}:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	}
	return nil, false
}

func repeated(values []string) string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return value
		}
		seen[value] = true
	}
	return ""
}
