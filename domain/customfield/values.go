package customfield

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"vozko/domain/shared"
)

const MultiSelectDelimiter = "|"

var ErrValueForbidden = errors.New("customfield: writing a sensitive field requires permission to read sensitive data")

type ValueError struct {
	Key string
	Err error
}

func (e *ValueError) Error() string {
	return fmt.Sprintf("%v (field %q)", e.Err, e.Key)
}

func (e *ValueError) Unwrap() error {
	return e.Err
}

func valueError(key string, err error) error {
	return &ValueError{Key: key, Err: err}
}

func byKey(defs []*Definition) map[string]*Definition {
	index := make(map[string]*Definition, len(defs))
	for _, d := range defs {
		if d != nil {
			index[d.Key] = d
		}
	}
	return index
}

func ValidateValues(defs []*Definition, values map[string]any) error {
	index := byKey(defs)
	for _, key := range slices.Sorted(maps.Keys(values)) {
		def, ok := index[key]
		if !ok {
			return valueError(key, ErrUnknownKey)
		}
		if err := def.ValidateValue(values[key]); err != nil {
			return valueError(key, err)
		}
	}
	for _, def := range defs {
		if def == nil {
			continue
		}
		if _, present := values[def.Key]; present {
			continue
		}
		if err := def.ValidateValue(nil); err != nil {
			return valueError(def.Key, err)
		}
	}
	return nil
}

func ApplyValues(defs []*Definition, viewer Viewer, current, patch map[string]any) (map[string]any, error) {
	index := byKey(defs)
	next := maps.Clone(current)
	if next == nil {
		next = map[string]any{}
	}
	for _, key := range slices.Sorted(maps.Keys(patch)) {
		def, ok := index[key]
		if !ok {
			return nil, valueError(key, ErrUnknownKey)
		}
		if !VisibleTo(def, viewer) {
			return nil, valueError(key, ErrValueForbidden)
		}
		value := patch[key]
		if value == nil {
			delete(next, key)
			continue
		}
		next[key] = value
	}
	if err := ValidateValues(visibleDefinitions(defs, viewer), VisibleValues(defs, viewer, next)); err != nil {
		return nil, err
	}
	if len(next) == 0 {
		return nil, nil
	}
	return next, nil
}

func ValidateOne(defs []*Definition, viewer Viewer, key string, value any) (*Definition, error) {
	def, ok := byKey(defs)[key]
	if !ok {
		return nil, valueError(key, ErrUnknownKey)
	}
	if !VisibleTo(def, viewer) {
		return nil, valueError(key, ErrValueForbidden)
	}
	if err := def.ValidateValue(value); err != nil {
		return nil, valueError(key, err)
	}
	return def, nil
}

func visibleDefinitions(defs []*Definition, viewer Viewer) []*Definition {
	visible := make([]*Definition, 0, len(defs))
	for _, d := range defs {
		if VisibleTo(d, viewer) {
			visible = append(visible, d)
		}
	}
	return visible
}

func VisibleValues(defs []*Definition, viewer Viewer, values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	index := byKey(defs)
	visible := make(map[string]any, len(values))
	for key, value := range values {
		if VisibleTo(index[key], viewer) {
			visible[key] = value
		}
	}
	return visible
}

func Recordable(defs []*Definition, key string) bool {
	def, ok := byKey(defs)[key]
	return ok && !def.Sensitive
}

func (d *Definition) Coerce(raw string) (any, error) {
	if d == nil {
		return nil, ErrUnknownKey
	}
	switch d.Type {
	case TypeNumber:
		f, err := shared.ParseNumberText(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: not a number: %q", ErrValueType, raw)
		}
		return f, nil
	case TypeBoolean:
		b, err := strconv.ParseBool(strings.ToLower(strings.TrimSpace(raw)))
		if err != nil {
			return nil, fmt.Errorf("%w: not a boolean: %q", ErrValueType, raw)
		}
		return b, nil
	case TypeMultiSelect:
		parts := strings.Split(raw, MultiSelectDelimiter)
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	default:
		return raw, nil
	}
}

func FormatValue(v any) string {
	return FormatValueJoined(v, MultiSelectDelimiter)
}

func FormatValueJoined(v any, separator string) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case []string:
		return strings.Join(val, separator)
	case []any:
		parts := make([]string, 0, len(val))
		for _, it := range val {
			parts = append(parts, fmt.Sprintf("%v", it))
		}
		return strings.Join(parts, separator)
	default:
		return fmt.Sprintf("%v", val)
	}
}
