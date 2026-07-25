package insurance

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type FieldType string

const (
	FieldTypeUnknown FieldType = "unknown"
	FieldTypeString  FieldType = "string"
	FieldTypeNumber  FieldType = "number"
	FieldTypeBoolean FieldType = "boolean"
	FieldTypeDate    FieldType = "date"
	FieldTypeArray   FieldType = "array"
	FieldTypeObject  FieldType = "object"
)

type RequiredField struct {
	Path                 string
	Alias                string
	Type                 FieldType
	Description          string
	Optional             bool
	Example              interface{}
	Providers            []InsuranceProvider
	AllowedValues        []string
	AllowedValuesAliases map[string]string
}

type RequirementSet struct {
	PolicyType PolicyType
	Providers  []InsuranceProvider
	Fields     []RequiredField
}

type fieldSegment struct {
	name    string
	isArray bool
}

func AggregateRequiredFields(policy PolicyType, providers []QuoteProvider) (RequirementSet, error) {
	merged := make(map[string]RequiredField)
	providerSet := make(map[InsuranceProvider]struct{})
	var providerList []InsuranceProvider

	for _, provider := range providers {
		if provider == nil {
			continue
		}
		if !provider.Supports(policy) {
			continue
		}

		name := provider.Provider()
		if _, ok := providerSet[name]; !ok {
			providerSet[name] = struct{}{}
			providerList = append(providerList, name)
		}

		fields, err := provider.RequiredFields(policy)
		if err != nil {
			return RequirementSet{}, fmt.Errorf("required fields for provider %s: %w", name, err)
		}

		for _, field := range fields {
			path := strings.TrimSpace(field.Path)
			if path == "" {
				continue
			}

			field.Path = path
			field.Providers = appendUniqueProviders(field.Providers, name)
			field.AllowedValues = normalizeAllowedValues(field.AllowedValues)

			existing, found := merged[path]
			if !found {
				merged[path] = field
				continue
			}

			if existing.Type == FieldTypeUnknown && field.Type != FieldTypeUnknown {
				existing.Type = field.Type
			}
			if existing.Description == "" {
				existing.Description = field.Description
			}
			if existing.Example == nil {
				existing.Example = field.Example
			}
			existing.Optional = existing.Optional && field.Optional
			existing.Providers = appendUniqueProviders(existing.Providers, name)
			existing.AllowedValues = appendUniqueStrings(existing.AllowedValues, field.AllowedValues)

			merged[path] = existing
		}
	}

	result := RequirementSet{
		PolicyType: policy,
		Providers:  append([]InsuranceProvider(nil), providerList...),
		Fields:     make([]RequiredField, 0, len(merged)),
	}

	for _, field := range merged {
		result.Fields = append(result.Fields, field)
	}

	sort.Slice(result.Providers, func(i, j int) bool {
		return result.Providers[i] < result.Providers[j]
	})
	result.Fields = sortedFields(result.Fields)

	return result, nil
}

func ValidateRequirements(details map[string]interface{}, required []RequiredField) []FieldViolation {
	if len(required) == 0 {
		return nil
	}

	var root interface{} = details
	if root == nil {
		root = map[string]interface{}{}
	}

	var violations []FieldViolation
	for _, field := range sortedFields(required) {
		if field.Optional {
			continue
		}
		values, missing := valuesForPath(root, field)
		if missing || !allValuesValid(values, field) {
			violations = append(violations, FieldViolation{
				Field:  field,
				Reason: violationReason(field),
			})
		}
	}

	return violations
}

func valuesForPath(root interface{}, field RequiredField) ([]interface{}, bool) {
	segments := parsePath(field.Path)
	if len(segments) == 0 {
		return nil, true
	}

	values := []interface{}{root}
	for idx, segment := range segments {
		last := idx == len(segments)-1
		next := make([]interface{}, 0, len(values))

		for _, node := range values {
			collectSegmentValues(node, segment, last, field.Type, &next)
		}

		if len(next) == 0 {
			return nil, true
		}
		values = next
	}

	return values, false
}

func collectSegmentValues(node interface{}, segment fieldSegment, last bool, expected FieldType, dest *[]interface{}) {
	if node == nil {
		return
	}

	switch current := node.(type) {
	case map[string]interface{}:
		val, ok := current[segment.name]
		if !ok || val == nil {
			return
		}
		handleSegmentValue(val, segment, last, expected, dest)
	case []interface{}:
		for _, item := range current {
			collectSegmentValues(item, segment, last, expected, dest)
		}
	default:
		if arr := toInterfaceSlice(node); arr != nil {
			for _, item := range arr {
				collectSegmentValues(item, segment, last, expected, dest)
			}
		}
	}
}

func handleSegmentValue(val interface{}, segment fieldSegment, last bool, expected FieldType, dest *[]interface{}) {
	if segment.isArray {
		items := toInterfaceSlice(val)
		if len(items) == 0 {
			return
		}

		if last && expected == FieldTypeArray {
			*dest = append(*dest, val)
			return
		}

		*dest = append(*dest, items...)
		return
	}

	*dest = append(*dest, val)
}

func parsePath(path string) []fieldSegment {
	parts := strings.Split(path, ".")
	segments := make([]fieldSegment, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		segment := fieldSegment{name: part}
		if strings.HasSuffix(part, "[]") {
			segment.name = strings.TrimSuffix(part, "[]")
			segment.isArray = true
		}

		segments = append(segments, segment)
	}

	return segments
}

func toInterfaceSlice(val interface{}) []interface{} {
	if val == nil {
		return nil
	}

	switch v := val.(type) {
	case []interface{}:
		return v
	}

	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}

	items := make([]interface{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		items[i] = rv.Index(i).Interface()
	}

	return items
}

func matchesFieldType(val interface{}, fieldType FieldType) bool {
	if val == nil {
		return false
	}

	switch fieldType {
	case FieldTypeString, FieldTypeDate:
		if str, ok := val.(string); ok {
			return strings.TrimSpace(str) != ""
		}
		return false
	case FieldTypeNumber:
		switch v := val.(type) {
		case json.Number:
			if _, err := v.Float64(); err == nil {
				return true
			}
			return false
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return true
		}
		rv := reflect.ValueOf(val)
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			return true
		default:
			return false
		}
	case FieldTypeBoolean:
		_, ok := val.(bool)
		return ok
	case FieldTypeArray:
		return len(toInterfaceSlice(val)) > 0
	case FieldTypeObject:
		if m, ok := val.(map[string]interface{}); ok {
			return len(m) > 0
		}
		return false
	default:
		return true
	}
}

func sortedFields(fields []RequiredField) []RequiredField {
	if len(fields) == 0 {
		return nil
	}

	out := append([]RequiredField(nil), fields...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Type < out[j].Type
		}
		return out[i].Path < out[j].Path
	})

	return out
}

func formatFieldPath(path string) string {
	if path == "" {
		return ""
	}

	parts := strings.Split(path, ".")
	for i, part := range parts {
		parts[i] = strings.TrimSuffix(part, "[]")
	}

	return strings.Join(parts, ".")
}

func appendUniqueProviders(providers []InsuranceProvider, provider InsuranceProvider) []InsuranceProvider {
	for _, existing := range providers {
		if existing == provider {
			return providers
		}
	}
	return append(providers, provider)
}

func normalizeAllowedValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{})

	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	if len(normalized) == 0 {
		return nil
	}

	sort.Strings(normalized)
	return normalized
}

func appendUniqueStrings(base []string, values []string) []string {
	if len(values) == 0 {
		return base
	}

	set := make(map[string]struct{}, len(base))
	for _, existing := range base {
		set[strings.ToLower(existing)] = struct{}{}
	}

	for _, value := range values {
		key := strings.ToLower(value)
		if _, ok := set[key]; ok {
			continue
		}
		set[key] = struct{}{}
		base = append(base, value)
	}

	if len(base) == 0 {
		return nil
	}

	sort.Strings(base)
	return base
}

func allValuesValid(values []interface{}, field RequiredField) bool {
	if len(values) == 0 {
		return false
	}

	for _, val := range values {
		if !valueMatchesField(val, field) {
			return false
		}
	}

	return true
}

func valueMatchesField(val interface{}, field RequiredField) bool {
	if !matchesFieldType(val, field.Type) {
		return false
	}

	if len(field.AllowedValues) == 0 {
		return true
	}

	str, ok := val.(string)
	if !ok {
		return false
	}

	str = strings.TrimSpace(str)
	if str == "" {
		return false
	}

	for _, allowed := range field.AllowedValues {
		if strings.EqualFold(str, allowed) {
			return true
		}
	}

	return false
}

func violationReason(field RequiredField) string {
	base := fmt.Sprintf("missing or invalid value for %s", formatFieldPath(field.Path))
	if len(field.AllowedValues) == 0 {
		return base
	}
	return fmt.Sprintf("%s; accepted values: %s", base, strings.Join(field.AllowedValues, ", "))
}
