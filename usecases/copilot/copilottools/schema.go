package copilottools

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"vozko/domain/tools"
)

func definition(name, description string, args any) tools.Definition {
	params, required := structParams(reflect.TypeOf(args), nil)
	return tools.Definition{Name: name, Description: description, Parameters: params, Required: required}
}

func structParams(t reflect.Type, descriptions map[string]string) (map[string]tools.Parameter, []string) {
	params := make(map[string]tools.Parameter)
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := jsonName(f)
		if name == "" {
			continue
		}
		description := descriptions[name]
		if description == "" {
			description = f.Tag.Get("desc")
		}
		param := tools.Parameter{Type: jsonType(f.Type), Description: description, Enum: enumValues(f)}
		if param.Type == "array" {
			param.Items = &tools.ParameterItems{Type: jsonType(elemType(f.Type))}
		}
		params[name] = param
		if f.Tag.Get("req") == "true" {
			required = append(required, name)
		}
	}
	return params, required
}

func enumValues(f reflect.StructField) []string {
	raw := f.Tag.Get("enum")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

func elemType(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice {
		return t.Elem()
	}
	return t
}

func jsonName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" || tag == "-" {
		return ""
	}
	if i := strings.IndexByte(tag, ','); i >= 0 {
		tag = tag[:i]
	}
	return tag
}

func jsonType(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int64, reflect.Float64:
		return "integer"
	case reflect.Slice:
		return "array"
	default:
		return "string"
	}
}

func bindArgs(args map[string]interface{}, dst any) {
	allowed := boundFields(reflect.TypeOf(dst).Elem())
	clean := make(map[string]interface{}, len(args))
	for k, v := range args {
		if _, ok := allowed[k]; ok {
			clean[k] = v
		}
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, dst)
}

func decodeArgs(args map[string]interface{}, dst any) error {
	bindArgs(args, dst)
	value := reflect.ValueOf(dst).Elem()
	t := value.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := jsonName(f)
		if name == "" {
			continue
		}
		field := value.Field(i)
		if f.Tag.Get("req") == "true" && blank(field) {
			return fmt.Errorf("%s é obrigatório", name)
		}
		if allowed := enumValues(f); allowed != nil && field.Kind() == reflect.String && field.String() != "" && !contains(allowed, field.String()) {
			return fmt.Errorf("%s deve ser um de: %s", name, strings.Join(allowed, ", "))
		}
	}
	return nil
}

func blank(v reflect.Value) bool {
	if v.Kind() == reflect.String {
		return strings.TrimSpace(v.String()) == ""
	}
	return v.IsZero()
}

func contains(values []string, v string) bool {
	for _, candidate := range values {
		if candidate == v {
			return true
		}
	}
	return false
}

func boundFields(t reflect.Type) map[string]struct{} {
	set := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if name := jsonName(t.Field(i)); name != "" {
			set[name] = struct{}{}
		}
	}
	return set
}
