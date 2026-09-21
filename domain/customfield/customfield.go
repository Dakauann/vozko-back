package customfield

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type FieldType string

const (
	TypeText        FieldType = "text"
	TypeNumber      FieldType = "number"
	TypeDate        FieldType = "date"
	TypeBoolean     FieldType = "boolean"
	TypeSelect      FieldType = "select"
	TypeMultiSelect FieldType = "multiselect"
)

func (t FieldType) Valid() bool {
	switch t {
	case TypeText, TypeNumber, TypeDate, TypeBoolean, TypeSelect, TypeMultiSelect:
		return true
	}
	return false
}

func (t FieldType) hasOptions() bool {
	return t == TypeSelect || t == TypeMultiSelect
}

type Definition struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	ObjectType  string    `json:"objectType"`
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	Type        FieldType `json:"type"`
	Options     []string  `json:"options,omitempty"`
	Required    bool      `json:"required"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

var (
	ErrWorkspaceRequired = errors.New("customfield: workspace is required")
	ErrKeyRequired       = errors.New("customfield: key is required")
	ErrLabelRequired     = errors.New("customfield: label is required")
	ErrInvalidType       = errors.New("customfield: invalid type")
	ErrOptionsRequired   = errors.New("customfield: select fields require options")
	ErrValueType         = errors.New("customfield: value does not match field type")
	ErrValueNotInOptions = errors.New("customfield: value is not an allowed option")
	ErrValueRequired     = errors.New("customfield: value is required")
)

func (d *Definition) Normalize() {
	d.Key = strings.TrimSpace(strings.ToLower(d.Key))
	d.Label = strings.TrimSpace(d.Label)
	d.ObjectType = strings.TrimSpace(strings.ToLower(d.ObjectType))
}

func (d *Definition) Validate() error {
	if strings.TrimSpace(d.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if strings.TrimSpace(d.Key) == "" {
		return ErrKeyRequired
	}
	if strings.TrimSpace(d.Label) == "" {
		return ErrLabelRequired
	}
	if !d.Type.Valid() {
		return ErrInvalidType
	}
	if d.Type.hasOptions() && len(d.Options) == 0 {
		return ErrOptionsRequired
	}
	return nil
}

func (d *Definition) ValidateValue(v any) error {
	if v == nil {
		if d.Required {
			return ErrValueRequired
		}
		return nil
	}
	switch d.Type {
	case TypeText:
		if _, ok := v.(string); !ok {
			return typeErr(d.Key, v)
		}
	case TypeNumber:
		if !isNumber(v) {
			return typeErr(d.Key, v)
		}
	case TypeDate:
		s, ok := v.(string)
		if !ok || !isISODate(s) {
			return typeErr(d.Key, v)
		}
	case TypeBoolean:
		if _, ok := v.(bool); !ok {
			return typeErr(d.Key, v)
		}
	case TypeSelect:
		s, ok := v.(string)
		if !ok {
			return typeErr(d.Key, v)
		}
		if !inOptions(d.Options, s) {
			return fmt.Errorf("%w: %q not in %v", ErrValueNotInOptions, s, d.Options)
		}
	case TypeMultiSelect:
		items, err := toStringSlice(v)
		if err != nil {
			return typeErr(d.Key, v)
		}
		for _, it := range items {
			if !inOptions(d.Options, it) {
				return fmt.Errorf("%w: %q not in %v", ErrValueNotInOptions, it, d.Options)
			}
		}
	default:
		return ErrInvalidType
	}
	return nil
}

func typeErr(key string, v any) error {
	return fmt.Errorf("%w: field %q got %T", ErrValueType, key, v)
}

func isNumber(v any) bool {
	switch n := v.(type) {
	case float64, float32, int, int64, int32:
		return true
	case json.Number:
		return true
	case string:
		_, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return err == nil
	}
	return false
}

func isISODate(s string) bool {
	s = strings.TrimSpace(s)
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func inOptions(options []string, v string) bool {
	for _, o := range options {
		if o == v {
			return true
		}
	}
	return false
}

func toStringSlice(v any) ([]string, error) {
	switch arr := v.(type) {
	case []string:
		return arr, nil
	case []any:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			s, ok := item.(string)
			if !ok {
				return nil, ErrValueType
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, ErrValueType
}
