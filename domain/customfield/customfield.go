package customfield

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
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

type ObjectType string

const (
	ObjectOpportunity ObjectType = "opportunity"
	ObjectLead        ObjectType = "lead"
)

func (o ObjectType) Valid() bool {
	return o == ObjectOpportunity || o == ObjectLead
}

func (o ObjectType) RequiresSensitivityChoice() bool {
	return o == ObjectLead
}

type Tone string

const (
	ToneChart1  Tone = "chart-1"
	ToneChart2  Tone = "chart-2"
	ToneChart3  Tone = "chart-3"
	ToneChart4  Tone = "chart-4"
	ToneChart5  Tone = "chart-5"
	ToneNeutral Tone = "neutral"
)

func (t Tone) Valid() bool {
	switch t {
	case ToneChart1, ToneChart2, ToneChart3, ToneChart4, ToneChart5, ToneNeutral:
		return true
	}
	return false
}

type Role string

const RoleClassification Role = "classification"

func (r Role) Valid() bool {
	return r == "" || r == RoleClassification
}

const MaxLegalBasisLength = 500

type Definition struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspaceId"`
	ObjectType  ObjectType      `json:"objectType"`
	Key         string          `json:"key"`
	Label       string          `json:"label"`
	Type        FieldType       `json:"type"`
	Options     []string        `json:"options,omitempty"`
	OptionTones map[string]Tone `json:"optionTones,omitempty"`
	Required    bool            `json:"required"`
	Sensitive   bool            `json:"sensitive"`
	LegalBasis  string          `json:"legalBasis,omitempty"`
	Role        Role            `json:"role,omitempty"`
	Position    int             `json:"position"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

var (
	ErrNotFound                   = errors.New("customfield: not found")
	ErrKeyExists                  = errors.New("customfield: key already exists for this object")
	ErrRoleTaken                  = errors.New("customfield: another field of this object already has this role")
	ErrInvalidDefinition          = errors.New("customfield: invalid definition")
	ErrWorkspaceRequired          = invalidDefinition("workspace is required")
	ErrInvalidObjectType          = invalidDefinition("object type must be opportunity or lead")
	ErrKeyRequired                = invalidDefinition("key is required")
	ErrLabelRequired              = invalidDefinition("label is required")
	ErrInvalidType                = invalidDefinition("invalid type")
	ErrOptionsRequired            = invalidDefinition("select fields require options")
	ErrSensitivityChoiceMissing   = invalidDefinition("say whether this field holds sensitive data")
	ErrLegalBasisRequired         = invalidDefinition("a sensitive field requires a legal basis")
	ErrLegalBasisTooLong          = invalidDefinition("legal basis is too long")
	ErrSensitiveUnsupportedObject = invalidDefinition("only lead fields may hold sensitive data")
	ErrInvalidTone                = invalidDefinition("tone must be chart-1 to chart-5 or neutral")
	ErrToneUnknownOption          = invalidDefinition("a tone names an option the field does not have")
	ErrInvalidRole                = invalidDefinition("unknown role")
	ErrRoleRequiresSelect         = invalidDefinition("the classification role requires a select field")
	ErrDefinitionsUnavailable     = errors.New("customfield: field definitions are unavailable")
	ErrUnknownKey                 = errors.New("customfield: unknown custom field")
	ErrValueType                  = errors.New("customfield: value does not match field type")
	ErrValueNotInOptions          = errors.New("customfield: value is not an allowed option")
	ErrValueRequired              = errors.New("customfield: value is required")
)

func (d *Definition) Normalize() {
	d.Key = strings.TrimSpace(strings.ToLower(d.Key))
	d.Label = strings.TrimSpace(d.Label)
	d.ObjectType = ObjectType(strings.TrimSpace(strings.ToLower(string(d.ObjectType))))
	d.Role = Role(strings.TrimSpace(strings.ToLower(string(d.Role))))
	d.LegalBasis = strings.TrimSpace(d.LegalBasis)
	if !d.Sensitive {
		d.LegalBasis = ""
	}
	if len(d.OptionTones) == 0 {
		d.OptionTones = nil
	}
}

func (d *Definition) Validate() error {
	if strings.TrimSpace(d.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if !d.ObjectType.Valid() {
		return ErrInvalidObjectType
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
	if err := d.validateSensitivity(); err != nil {
		return err
	}
	if err := d.validateTones(); err != nil {
		return err
	}
	return d.validateRole()
}

func invalidDefinition(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidDefinition, reason)
}

func (d *Definition) validateSensitivity() error {
	if !d.Sensitive {
		return nil
	}
	if !d.ObjectType.RequiresSensitivityChoice() {
		return ErrSensitiveUnsupportedObject
	}
	if strings.TrimSpace(d.LegalBasis) == "" {
		return ErrLegalBasisRequired
	}
	if utf8.RuneCountInString(d.LegalBasis) > MaxLegalBasisLength {
		return ErrLegalBasisTooLong
	}
	return nil
}

func (d *Definition) validateTones() error {
	for option, tone := range d.OptionTones {
		if !d.Type.hasOptions() || !inOptions(d.Options, option) {
			return fmt.Errorf("%w: %q", ErrToneUnknownOption, option)
		}
		if !tone.Valid() {
			return fmt.Errorf("%w: %q", ErrInvalidTone, tone)
		}
	}
	return nil
}

func (d *Definition) validateRole() error {
	if !d.Role.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidRole, d.Role)
	}
	if d.Role == RoleClassification && d.Type != TypeSelect {
		return ErrRoleRequiresSelect
	}
	return nil
}

func (d *Definition) ReplaceOptions(options []string) {
	d.Options = options
	for option := range d.OptionTones {
		if !inOptions(options, option) {
			delete(d.OptionTones, option)
		}
	}
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

type Viewer struct {
	ReadsSensitive bool
}

func VisibleTo(def *Definition, viewer Viewer) bool {
	if def == nil {
		return false
	}
	return !def.Sensitive || viewer.ReadsSensitive
}

func typeErr(key string, v any) error {
	return fmt.Errorf("%w: field %q got %T", ErrValueType, key, v)
}

func isNumber(v any) bool {
	switch n := v.(type) {
	case float64:
		return isFinite(n)
	case float32:
		return isFinite(float64(n))
	case int, int64, int32:
		return true
	case json.Number:
		return isStoredNumberText(n.String())
	case string:
		return isStoredNumberText(n)
	}
	return false
}

func isFinite(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0)
}

func isStoredNumberText(raw string) bool {
	_, err := shared.ParseNumberText(raw)
	return err == nil
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
