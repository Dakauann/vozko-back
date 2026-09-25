package copilot

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const MaxFieldRunes = 280

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Describer interface {
	Describe(ctx context.Context, cc Context, args map[string]interface{}) []Field
}

func DescribeArgs(args map[string]interface{}) []Field {
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []Field{}
	for _, key := range keys {
		if identifierKey(key) {
			continue
		}
		if value, ok := plainValue(args[key]); ok {
			out = append(out, Field{Key: key, Value: trimField(value)})
		}
	}
	return out
}

func identifierKey(key string) bool {
	if key == "id" || strings.HasSuffix(key, "_id") || strings.HasSuffix(key, "_ids") {
		return true
	}
	for _, suffix := range []string{"Id", "Ids", "ID", "IDs"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func plainValue(raw interface{}) (string, bool) {
	switch v := raw.(type) {
	case string:
		s := strings.TrimSpace(v)
		return s, s != "" && !uuidPattern.MatchString(s)
	case bool:
		return strconv.FormatBool(v), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case int:
		return strconv.Itoa(v), true
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := plainValue(item); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", "), len(parts) > 0
	}
	return "", false
}

func trimField(s string) string {
	runes := []rune(s)
	if len(runes) <= MaxFieldRunes {
		return s
	}
	return string(runes[:MaxFieldRunes]) + "…"
}

type Validator interface {
	Validate(ctx context.Context, cc Context, args map[string]interface{}) error
}
