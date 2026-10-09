package customfield

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"vozko/domain/crmfilter"
	"vozko/domain/shared"
)

var (
	ErrFilterUnknownKey = errors.New("customfield: filter names an unknown custom field")
	ErrFilterOperator   = errors.New("customfield: operator does not suit the custom field type")
	ErrFilterValue      = errors.New("customfield: filter value does not suit the custom field type")
	ErrFilterSensitive  = errors.New("customfield: filtering on a sensitive field requires permission to read sensitive data")
)

type FilterError struct {
	Key      string
	Operator crmfilter.Operator
	Err      error
}

func (e *FilterError) Error() string {
	return fmt.Sprintf("%v: key %q, operator %q", e.Err, e.Key, e.Operator)
}

func (e *FilterError) Unwrap() error { return e.Err }

var (
	textOperators      = []crmfilter.Operator{crmfilter.OpEquals, crmfilter.OpNotEquals, crmfilter.OpIn, crmfilter.OpNotIn, crmfilter.OpContains, crmfilter.OpIsSet, crmfilter.OpIsEmpty}
	numberOperators    = []crmfilter.Operator{crmfilter.OpEquals, crmfilter.OpNotEquals, crmfilter.OpGreaterEq, crmfilter.OpLessEq, crmfilter.OpBetween, crmfilter.OpIsSet, crmfilter.OpIsEmpty}
	dateOperators      = []crmfilter.Operator{crmfilter.OpBefore, crmfilter.OpAfter, crmfilter.OpGreaterEq, crmfilter.OpLessEq, crmfilter.OpBetween, crmfilter.OpIsSet, crmfilter.OpIsEmpty}
	booleanOperators   = []crmfilter.Operator{crmfilter.OpIsTrue, crmfilter.OpIsFalse, crmfilter.OpEquals, crmfilter.OpIsSet, crmfilter.OpIsEmpty}
	optionOperators    = []crmfilter.Operator{crmfilter.OpEquals, crmfilter.OpNotEquals, crmfilter.OpIn, crmfilter.OpNotIn, crmfilter.OpIsSet, crmfilter.OpIsEmpty}
	valuelessOperators = []crmfilter.Operator{crmfilter.OpIsSet, crmfilter.OpIsEmpty, crmfilter.OpIsTrue, crmfilter.OpIsFalse}
)

func (t FieldType) FilterOperators() []crmfilter.Operator {
	switch t {
	case TypeText:
		return slices.Clone(textOperators)
	case TypeNumber:
		return slices.Clone(numberOperators)
	case TypeDate:
		return slices.Clone(dateOperators)
	case TypeBoolean:
		return slices.Clone(booleanOperators)
	case TypeSelect, TypeMultiSelect:
		return slices.Clone(optionOperators)
	}
	return nil
}

func (t FieldType) FilterKind() (crmfilter.Kind, bool) {
	switch t {
	case TypeText:
		return crmfilter.KindString, true
	case TypeNumber:
		return crmfilter.KindNumber, true
	case TypeDate:
		return crmfilter.KindDate, true
	case TypeBoolean:
		return crmfilter.KindBool, true
	case TypeSelect:
		return crmfilter.KindEnum, true
	case TypeMultiSelect:
		return crmfilter.KindMultiEnum, true
	}
	return 0, false
}

func BindFilter(filter crmfilter.Filter, defs []*Definition, viewer Viewer) (crmfilter.Filter, error) {
	if err := filter.Validate(); err != nil {
		return crmfilter.Filter{}, err
	}
	index := byKey(defs)
	bound := crmfilter.Filter{Groups: make([]crmfilter.Group, len(filter.Groups))}
	for gi, g := range filter.Groups {
		preds := make([]crmfilter.Predicate, len(g.Predicates))
		for pi, p := range g.Predicates {
			if p.Field != crmfilter.FieldCustom {
				preds[pi] = p
				continue
			}
			bp, err := bindPredicate(p, index, viewer)
			if err != nil {
				return crmfilter.Filter{}, err
			}
			preds[pi] = bp
		}
		bound.Groups[gi] = crmfilter.Group{Conjunction: g.Conjunction, Predicates: preds}
	}
	if filter.Groups == nil {
		bound.Groups = nil
	}
	return bound, nil
}

func bindPredicate(p crmfilter.Predicate, byKey map[string]*Definition, viewer Viewer) (crmfilter.Predicate, error) {
	key := strings.TrimSpace(strings.ToLower(p.Key))
	refuse := func(err error) (crmfilter.Predicate, error) {
		return crmfilter.Predicate{}, &FilterError{Key: key, Operator: p.Operator, Err: err}
	}
	def, ok := byKey[key]
	if !ok {
		return refuse(ErrFilterUnknownKey)
	}
	if !VisibleTo(def, viewer) {
		return refuse(ErrFilterSensitive)
	}
	if !slices.Contains(def.Type.FilterOperators(), p.Operator) {
		return refuse(ErrFilterOperator)
	}
	kind, ok := def.Type.FilterKind()
	if !ok {
		return refuse(ErrFilterOperator)
	}
	if !slices.Contains(valuelessOperators, p.Operator) && !valuesSuit(kind, p.Values) {
		return refuse(ErrFilterValue)
	}
	p.Key = key
	return p.BindKind(kind), nil
}

func valuesSuit(kind crmfilter.Kind, values []string) bool {
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		switch kind {
		case crmfilter.KindNumber:
			if _, err := shared.ParseNumberText(v); err != nil {
				return false
			}
		case crmfilter.KindDate:
			if _, err := crmfilter.ParseDate(v); err != nil {
				return false
			}
		case crmfilter.KindBool:
			if _, err := strconv.ParseBool(v); err != nil {
				return false
			}
		}
	}
	return true
}
