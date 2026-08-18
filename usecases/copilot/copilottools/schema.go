package copilottools

import (
	"encoding/json"
	"reflect"
	"strings"

	"vozko/domain/tools"
)

// structParams builds a tool schema by reflection. It is only ever pointed at
// a DTO owned by this package (see agentFields): reflecting off a domain input
// struct made the schema a hostage of json tags written for HTTP binding, and
// every `json:"-"` field silently disappeared from what the model could do.
func structParams(t reflect.Type, descriptions map[string]string) (map[string]tools.Parameter, []string) {
	params := make(map[string]tools.Parameter)
	var required []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := jsonName(f)
		if name == "" {
			continue
		}
		params[name] = tools.Parameter{Type: jsonType(f.Type), Description: descriptions[name]}
		if f.Tag.Get("req") == "true" {
			required = append(required, name)
		}
	}
	return params, required
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

func boundFields(t reflect.Type) map[string]struct{} {
	set := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		if name := jsonName(t.Field(i)); name != "" {
			set[name] = struct{}{}
		}
	}
	return set
}
