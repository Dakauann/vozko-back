package tooltest

import (
	"context"
	"testing"

	"vozko/domain/copilot"
)

func FieldValue(fields []copilot.Field, key string) string {
	for _, f := range fields {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

func HasField(fields []copilot.Field, key, value string) bool {
	for _, f := range fields {
		if f.Key == key && f.Value == value {
			return true
		}
	}
	return false
}

func FieldMap(fields []copilot.Field) map[string]string {
	out := map[string]string{}
	for _, f := range fields {
		out[f.Key] = f.Value
	}
	return out
}

func DataOf(res copilot.Result) map[string]interface{} {
	data, _ := res.Data.(map[string]interface{})
	return data
}

func Validate(t *testing.T, tool copilot.Tool, cc copilot.Context, args map[string]interface{}) error {
	t.Helper()
	v, ok := tool.(copilot.Validator)
	if !ok {
		t.Fatalf("%s has no preflight", tool.Definition().Name)
	}
	return v.Validate(context.Background(), cc, args)
}
