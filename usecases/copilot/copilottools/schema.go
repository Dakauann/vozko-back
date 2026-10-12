package copilottools

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/tools"
)

func Definition(name, description string, args any) tools.Definition {
	params, required := StructParams(reflect.TypeOf(args), nil)
	return tools.Definition{Name: name, Description: description, Parameters: params, Required: required}
}

func StructParams(t reflect.Type, descriptions map[string]string) (map[string]tools.Parameter, []string) {
	params := make(map[string]tools.Parameter)
	var required []string
	for _, f := range reflect.VisibleFields(t) {
		name := JSONName(f)
		if name == "" {
			continue
		}
		description := descriptions[name]
		if description == "" {
			description = f.Tag.Get("desc")
		}
		param := tools.Parameter{Type: jsonType(f.Type), Description: description, Enum: EnumValues(f)}
		switch param.Type {
		case "array":
			param.Items = arrayItems(elemType(f.Type))
			if leaf := innermostItems(param.Items); leaf.Type != "object" {
				leaf.Enum, param.Enum = param.Enum, nil
			}
		case "object":
			param.Items = arrayItems(elemType(f.Type))
		}
		params[name] = param
		if f.Tag.Get("req") == "true" {
			required = append(required, name)
		}
	}
	return params, required
}

func arrayItems(elem reflect.Type) *tools.ParameterItems {
	if elem.Kind() == reflect.Ptr {
		elem = elem.Elem()
	}
	switch elem.Kind() {
	case reflect.Struct:
		properties, required := StructParams(elem, nil)
		return &tools.ParameterItems{Type: "object", Properties: properties, Required: required}
	case reflect.Slice:
		return &tools.ParameterItems{Type: "array", Items: arrayItems(elem.Elem())}
	}
	return &tools.ParameterItems{Type: jsonType(elem)}
}

func innermostItems(items *tools.ParameterItems) *tools.ParameterItems {
	for items.Items != nil {
		items = items.Items
	}
	return items
}

func EnumValues(f reflect.StructField) []string {
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

func JSONName(f reflect.StructField) string {
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
	case reflect.Int, reflect.Int64:
		return "integer"
	case reflect.Float64:
		return "number"
	case reflect.Slice:
		return "array"
	case reflect.Struct:
		return "object"
	default:
		return "string"
	}
}

func BindArgs(args map[string]interface{}, dst any) {
	allowed := BoundFields(reflect.TypeOf(dst).Elem())
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

func DecodeArgs(args map[string]interface{}, dst any) error {
	BindArgs(args, dst)
	value := reflect.ValueOf(dst).Elem()
	t := value.Type()
	for _, f := range reflect.VisibleFields(t) {
		name := JSONName(f)
		if name == "" {
			continue
		}
		field := value.FieldByIndex(f.Index)
		if f.Tag.Get("req") == "true" && blank(field) {
			return fmt.Errorf("%s é obrigatório", name)
		}
		if err := knownIDs(f, field); err != nil {
			return err
		}
		if allowed := EnumValues(f); allowed != nil && field.Kind() == reflect.String && field.String() != "" && !Contains(allowed, field.String()) {
			return fmt.Errorf("%s deve ser um de: %s", name, strings.Join(allowed, ", "))
		}
	}
	return nil
}

func knownIDs(f reflect.StructField, field reflect.Value) error {
	if f.Tag.Get("id") != "true" {
		return nil
	}
	name := JSONName(f)
	switch field.Kind() {
	case reflect.String:
		return shapedID(field, name)
	case reflect.Slice:
		for i := 0; i < field.Len(); i++ {
			if err := shapedID(field.Index(i), name); err != nil {
				return err
			}
		}
	}
	return nil
}

func shapedID(v reflect.Value, name string) error {
	raw := strings.TrimSpace(v.String())
	if raw == "" {
		return nil
	}
	if _, err := uuid.Parse(raw); err != nil {
		return fmt.Errorf("%w: %s %q não existe; copie o id exato devolvido pelas ferramentas, nunca invente", ErrInvalidArgs, name, raw)
	}
	if v.CanSet() {
		v.SetString(raw)
	}
	return nil
}

func blank(v reflect.Value) bool {
	if v.Kind() == reflect.String {
		return strings.TrimSpace(v.String()) == ""
	}
	return v.IsZero()
}

func Contains(values []string, v string) bool {
	for _, candidate := range values {
		if candidate == v {
			return true
		}
	}
	return false
}

func BoundFields(t reflect.Type) map[string]struct{} {
	set := make(map[string]struct{}, t.NumField())
	for _, f := range reflect.VisibleFields(t) {
		if name := JSONName(f); name != "" {
			set[name] = struct{}{}
		}
	}
	return set
}
