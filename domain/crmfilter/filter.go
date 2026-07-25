// Package crmfilter defines a reusable, object-agnostic filter value object
// shared across the CRM read model (conversation entries today, sales
// opportunities next). It is pure domain: it describes WHAT to filter, never
// HOW to query. The infra FilterCompiler maps a Filter onto concrete SQL per
// object, which is what lets the kanban board, the list view and the column
// summaries all read from a single filter definition instead of the two
// duplicated hand-written SQL builders that exist today.
package crmfilter

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Field identifies a filterable attribute. Fields are intentionally
// object-agnostic; the compiler resolves each one to the right column/join for
// the object being queried.
type Field string

const (
	FieldOwner          Field = "owner"
	FieldCarteira       Field = "carteira"
	FieldPipeline       Field = "pipeline"
	FieldStage          Field = "stage"
	FieldLabel          Field = "label"
	FieldValue          Field = "value"
	FieldCreatedAt      Field = "created_at"
	FieldUpdatedAt      Field = "updated_at"
	FieldCloseDate      Field = "close_date"
	FieldLastActivityAt Field = "last_activity_at"
	FieldSource         Field = "source"
	FieldLostReason     Field = "lost_reason"
	FieldChannel        Field = "channel"
	FieldCampaign       Field = "campaign"
	FieldStatus         Field = "status"
	FieldUnread         Field = "unread"
	FieldWindowOpen     Field = "window_open"
	FieldQuery          Field = "query"
	// FieldCustom targets a typed custom field. Predicate.Key must name the
	// custom field; its value kind is resolved by the compiler from the
	// custom-field definition, so validation here is intentionally permissive.
	FieldCustom Field = "custom"
)

// Operator is the comparison applied to a field.
type Operator string

const (
	OpEquals    Operator = "eq"
	OpNotEquals Operator = "neq"
	OpIn        Operator = "in"
	OpNotIn     Operator = "not_in"
	OpContains  Operator = "contains"
	OpGreaterEq Operator = "gte"
	OpLessEq    Operator = "lte"
	OpBetween   Operator = "between"
	OpBefore    Operator = "before"
	OpAfter     Operator = "after"
	OpIsSet     Operator = "is_set"
	OpIsEmpty   Operator = "is_empty"
	OpIsTrue    Operator = "is_true"
	OpIsFalse   Operator = "is_false"
)

// Kind is the value type of a field, used for validation and by the compiler.
type Kind int

const (
	KindString Kind = iota
	KindNumber
	KindDate
	KindBool
	KindIDSet // references to entity ids (owner, stage, label, pipeline, ...)
	KindEnum
	KindText // free-text search
)

// Conjunction combines predicates.
type Conjunction string

const (
	And Conjunction = "and"
	Or  Conjunction = "or"
)

// Predicate is one comparison. Values are string-encoded (numbers and dates as
// text) so a Filter serializes cleanly to JSON for saved views; the compiler
// parses them per the field Kind.
type Predicate struct {
	Field    Field    `json:"field"`
	Key      string   `json:"key,omitempty"` // only for FieldCustom
	Operator Operator `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

// Group is a set of predicates combined with Conjunction (defaults to Or).
type Group struct {
	Conjunction Conjunction `json:"conjunction,omitempty"`
	Predicates  []Predicate `json:"predicates"`
}

// Conj returns the group's conjunction, defaulting to Or when unset. Groups
// default to OR internally; the top-level Groups always combine with AND. That
// yields the "AND across filters, OR within a group" model buyers expect
// (e.g. stage=Proposal AND (source=whatsapp OR source=instagram)).
func (g Group) Conj() Conjunction {
	if g.Conjunction == And {
		return And
	}
	return Or
}

// Filter is the whole expression: Groups combined with AND.
type Filter struct {
	Groups []Group `json:"groups"`
}

// FieldSpec is the static metadata for a field.
type FieldSpec struct {
	Field Field
	Kind  Kind
	Ops   []Operator
	Multi bool // supports multi-value IN / NOT IN
}

var (
	ErrUnknownField     = errors.New("crmfilter: unknown field")
	ErrUnsupportedOp    = errors.New("crmfilter: unsupported operator for field")
	ErrMissingValue     = errors.New("crmfilter: predicate requires at least one value")
	ErrBetweenValues    = errors.New("crmfilter: between requires exactly two values")
	ErrInvalidNumber    = errors.New("crmfilter: value is not a valid number")
	ErrInvalidDate      = errors.New("crmfilter: value is not a valid date")
	ErrMissingCustomKey = errors.New("crmfilter: custom field requires a key")
)

var (
	idSetOps  = []Operator{OpIn, OpNotIn, OpEquals, OpNotEquals, OpIsSet, OpIsEmpty}
	enumOps   = []Operator{OpEquals, OpNotEquals, OpIn, OpNotIn, OpIsSet, OpIsEmpty}
	numberOps = []Operator{OpEquals, OpNotEquals, OpGreaterEq, OpLessEq, OpBetween, OpIsSet, OpIsEmpty}
	dateOps   = []Operator{OpBefore, OpAfter, OpBetween, OpGreaterEq, OpLessEq, OpIsSet, OpIsEmpty}
	boolOps   = []Operator{OpIsTrue, OpIsFalse, OpEquals}
	textOps   = []Operator{OpContains}
	customOps = []Operator{OpEquals, OpNotEquals, OpIn, OpNotIn, OpContains, OpGreaterEq, OpLessEq, OpBetween, OpIsSet, OpIsEmpty}
)

var registry = map[Field]FieldSpec{
	FieldOwner:          {FieldOwner, KindIDSet, idSetOps, true},
	FieldCarteira:       {FieldCarteira, KindIDSet, idSetOps, true},
	FieldPipeline:       {FieldPipeline, KindIDSet, idSetOps, true},
	FieldStage:          {FieldStage, KindIDSet, idSetOps, true},
	FieldLabel:          {FieldLabel, KindIDSet, idSetOps, true},
	FieldLostReason:     {FieldLostReason, KindIDSet, idSetOps, true},
	FieldCampaign:       {FieldCampaign, KindIDSet, idSetOps, true},
	FieldSource:         {FieldSource, KindEnum, enumOps, true},
	FieldChannel:        {FieldChannel, KindEnum, enumOps, true},
	FieldStatus:         {FieldStatus, KindEnum, enumOps, true},
	FieldValue:          {FieldValue, KindNumber, numberOps, false},
	FieldCreatedAt:      {FieldCreatedAt, KindDate, dateOps, false},
	FieldUpdatedAt:      {FieldUpdatedAt, KindDate, dateOps, false},
	FieldCloseDate:      {FieldCloseDate, KindDate, dateOps, false},
	FieldLastActivityAt: {FieldLastActivityAt, KindDate, dateOps, false},
	FieldUnread:         {FieldUnread, KindBool, boolOps, false},
	FieldWindowOpen:     {FieldWindowOpen, KindBool, boolOps, false},
	FieldQuery:          {FieldQuery, KindText, textOps, false},
	FieldCustom:         {FieldCustom, KindString, customOps, true},
}

// SpecFor returns the static spec for a field.
func SpecFor(f Field) (FieldSpec, bool) {
	spec, ok := registry[f]
	return spec, ok
}

// IsEmpty reports whether the filter has no predicates.
func (f Filter) IsEmpty() bool {
	for _, g := range f.Groups {
		if len(g.Predicates) > 0 {
			return false
		}
	}
	return true
}

// Fields returns the unique set of fields referenced, so the compiler can plan
// exactly the joins it needs and nothing more.
func (f Filter) Fields() []Field {
	seen := map[Field]struct{}{}
	var out []Field
	for _, g := range f.Groups {
		for _, p := range g.Predicates {
			if _, ok := seen[p.Field]; ok {
				continue
			}
			seen[p.Field] = struct{}{}
			out = append(out, p.Field)
		}
	}
	return out
}

// Validate checks every predicate against its field spec.
func (f Filter) Validate() error {
	for gi := range f.Groups {
		for pi := range f.Groups[gi].Predicates {
			if err := f.Groups[gi].Predicates[pi].Validate(); err != nil {
				return fmt.Errorf("group %d predicate %d: %w", gi, pi, err)
			}
		}
	}
	return nil
}

// Validate checks a single predicate for field/operator/value coherence.
func (p Predicate) Validate() error {
	spec, ok := registry[p.Field]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownField, p.Field)
	}
	if p.Field == FieldCustom && strings.TrimSpace(p.Key) == "" {
		return ErrMissingCustomKey
	}
	if !opAllowed(spec.Ops, p.Operator) {
		return fmt.Errorf("%w: %q on %q", ErrUnsupportedOp, p.Operator, p.Field)
	}

	switch p.Operator {
	case OpIsSet, OpIsEmpty, OpIsTrue, OpIsFalse:
		return nil // presence/boolean operators take no value
	case OpBetween:
		if len(nonEmpty(p.Values)) != 2 {
			return ErrBetweenValues
		}
	default:
		if len(nonEmpty(p.Values)) == 0 {
			return ErrMissingValue
		}
	}

	// FieldCustom kind is resolved at compile time; skip static typing here.
	if p.Field == FieldCustom {
		return nil
	}
	switch spec.Kind {
	case KindNumber:
		for _, v := range p.Values {
			if strings.TrimSpace(v) == "" {
				continue
			}
			if _, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err != nil {
				return fmt.Errorf("%w: %q", ErrInvalidNumber, v)
			}
		}
	case KindDate:
		for _, v := range p.Values {
			if strings.TrimSpace(v) == "" {
				continue
			}
			if _, err := ParseDate(v); err != nil {
				return err
			}
		}
	}
	return nil
}

// ParseDate accepts RFC3339 timestamps and bare YYYY-MM-DD dates.
func ParseDate(v string) (time.Time, error) {
	s := strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidDate, v)
}

func opAllowed(allowed []Operator, op Operator) bool {
	for _, a := range allowed {
		if a == op {
			return true
		}
	}
	return false
}

func nonEmpty(values []string) []string {
	out := values[:0:0]
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
