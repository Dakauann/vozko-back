package crmfilter

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

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
	FieldName           Field = "name"
	FieldNumber         Field = "number"
	FieldAge            Field = "age"
	FieldBlocked        Field = "blocked"

	FieldCampaignStatus Field = "campaign_status"
	FieldCampaignCount  Field = "campaign_count"

	FieldMemoryCategory  Field = "memory_category"
	FieldMemoryAuthor    Field = "memory_author"
	FieldMemoryText      Field = "memory_text"
	FieldMemoryCount     Field = "memory_count"
	FieldMemoryUpdatedAt Field = "memory_updated_at"

	FieldCustom Field = "custom"
)

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

type Kind int

const (
	KindString Kind = iota
	KindNumber
	KindDate
	KindBool
	KindIDSet
	KindEnum
	KindText
)

type Conjunction string

const (
	And Conjunction = "and"
	Or  Conjunction = "or"
)

type Predicate struct {
	Field    Field    `json:"field"`
	Key      string   `json:"key,omitempty"`
	Operator Operator `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

type Group struct {
	Conjunction Conjunction `json:"conjunction,omitempty"`
	Predicates  []Predicate `json:"predicates"`
}

func (g Group) Conj() Conjunction {
	if g.Conjunction == And {
		return And
	}
	return Or
}

type Filter struct {
	Groups []Group `json:"groups"`
}

type FieldSpec struct {
	Field Field
	Kind  Kind
	Ops   []Operator
	Multi bool
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
	stringOps = []Operator{OpEquals, OpNotEquals, OpIn, OpNotIn, OpContains, OpIsSet, OpIsEmpty}
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

	FieldName:            {FieldName, KindString, stringOps, true},
	FieldNumber:          {FieldNumber, KindString, stringOps, true},
	FieldAge:             {FieldAge, KindNumber, numberOps, false},
	FieldBlocked:         {FieldBlocked, KindBool, boolOps, false},
	FieldCampaignStatus:  {FieldCampaignStatus, KindEnum, enumOps, true},
	FieldCampaignCount:   {FieldCampaignCount, KindNumber, numberOps, false},
	FieldMemoryCategory:  {FieldMemoryCategory, KindEnum, enumOps, true},
	FieldMemoryAuthor:    {FieldMemoryAuthor, KindEnum, enumOps, true},
	FieldMemoryText:      {FieldMemoryText, KindText, textOps, false},
	FieldMemoryCount:     {FieldMemoryCount, KindNumber, numberOps, false},
	FieldMemoryUpdatedAt: {FieldMemoryUpdatedAt, KindDate, dateOps, false},

	FieldCustom: {FieldCustom, KindString, customOps, true},
}

func SpecFor(f Field) (FieldSpec, bool) {
	spec, ok := registry[f]
	return spec, ok
}

func (f Filter) IsEmpty() bool {
	for _, g := range f.Groups {
		if len(g.Predicates) > 0 {
			return false
		}
	}
	return true
}

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
		return nil
	case OpBetween:
		if len(nonEmpty(p.Values)) != 2 {
			return ErrBetweenValues
		}
	default:
		if len(nonEmpty(p.Values)) == 0 {
			return ErrMissingValue
		}
	}

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
