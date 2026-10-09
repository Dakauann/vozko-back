package crmfilter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"vozko/domain/crmfilter"
	"vozko/infra/database"

	"github.com/lib/pq"
)

var ErrUnboundCustomField = fmt.Errorf("%w: custom field predicate was not bound to its definition", crmfilter.ErrNotApplicable)

type CustomFieldsDescriptor interface {
	CustomFieldsColumn() string
}

const emptyJSONBValues = "('null'::jsonb, '\"\"'::jsonb, '[]'::jsonb)"

func compileJSONBCustom(p crmfilter.Predicate, column string) (string, []interface{}, error) {
	kind, bound := p.BoundKind()
	if !bound {
		return "", nil, fmt.Errorf("%w: %q", ErrUnboundCustomField, p.Key)
	}
	key := strings.TrimSpace(p.Key)
	if key == "" {
		return "", nil, crmfilter.ErrMissingCustomKey
	}
	switch p.Operator {
	case crmfilter.OpIsSet:
		return "COALESCE(" + column + " -> ?::text, 'null'::jsonb) NOT IN " + emptyJSONBValues, []interface{}{key}, nil
	case crmfilter.OpIsEmpty:
		return "COALESCE(" + column + " -> ?::text, 'null'::jsonb) IN " + emptyJSONBValues, []interface{}{key}, nil
	}
	vals := trimmedValues(p.Values)
	switch kind {
	case crmfilter.KindString, crmfilter.KindEnum:
		return compileJSONBOption(p.Operator, column, key, vals, kind)
	case crmfilter.KindMultiEnum:
		return compileJSONBMultiOption(p.Operator, column, key, vals)
	case crmfilter.KindNumber:
		return numericRange.compile(p.Operator, column, key, vals)
	case crmfilter.KindDate:
		return dateRange.compile(p.Operator, column, key, vals)
	case crmfilter.KindBool:
		return compileJSONBBool(p, column, key, vals)
	}
	return "", nil, unsupportedCustom(p.Operator, kind)
}

func compileJSONBOption(op crmfilter.Operator, column, key string, vals []string, kind crmfilter.Kind) (string, []interface{}, error) {
	equals := column + " @> jsonb_build_object(?::text, ?::text)"
	switch op {
	case crmfilter.OpEquals:
		return equals, []interface{}{key, vals[0]}, nil
	case crmfilter.OpNotEquals:
		return negate(equals), []interface{}{key, vals[0]}, nil
	case crmfilter.OpIn, crmfilter.OpNotIn:
		docs, err := containmentDocs(vals, func(v string) any { return map[string]string{key: v} })
		return anyOfContainment(op, column, docs, err)
	case crmfilter.OpContains:
		if kind == crmfilter.KindString {
			return "(" + column + " ->> ?::text) ILIKE ?::text", []interface{}{key, database.LikeContains(vals[0])}, nil
		}
	}
	return "", nil, unsupportedCustom(op, kind)
}

func compileJSONBMultiOption(op crmfilter.Operator, column, key string, vals []string) (string, []interface{}, error) {
	equals := column + " @> jsonb_build_object(?::text, jsonb_build_array(?::text))"
	switch op {
	case crmfilter.OpEquals:
		return equals, []interface{}{key, vals[0]}, nil
	case crmfilter.OpNotEquals:
		return negate(equals), []interface{}{key, vals[0]}, nil
	case crmfilter.OpIn, crmfilter.OpNotIn:
		docs, err := containmentDocs(vals, func(v string) any { return map[string][]string{key: {v}} })
		return anyOfContainment(op, column, docs, err)
	}
	return "", nil, unsupportedCustom(op, crmfilter.KindMultiEnum)
}

func anyOfContainment(op crmfilter.Operator, column string, docs []string, err error) (string, []interface{}, error) {
	if err != nil {
		return "", nil, err
	}
	anyOf := column + " @> ANY(?::jsonb[])"
	args := []interface{}{pq.Array(docs)}
	if op == crmfilter.OpNotIn {
		return negate(anyOf), args, nil
	}
	return anyOf, args, nil
}

func containmentDocs(vals []string, doc func(string) any) ([]string, error) {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		raw, err := json.Marshal(doc(v))
		if err != nil {
			return nil, err
		}
		out = append(out, string(raw))
	}
	return out, nil
}

type jsonbRange struct {
	kind        crmfilter.Kind
	cast        string
	comparisons map[crmfilter.Operator]string
	arg         func(string) (interface{}, error)
}

var numericRange = jsonbRange{
	kind: crmfilter.KindNumber,
	cast: "numeric",
	comparisons: map[crmfilter.Operator]string{
		crmfilter.OpEquals: " = ", crmfilter.OpNotEquals: " = ", crmfilter.OpGreaterEq: " >= ", crmfilter.OpLessEq: " <= ",
	},
	arg: numberArg,
}

var dateRange = jsonbRange{
	kind: crmfilter.KindDate,
	cast: "date",
	comparisons: map[crmfilter.Operator]string{
		crmfilter.OpBefore: " < ", crmfilter.OpAfter: " > ", crmfilter.OpGreaterEq: " >= ", crmfilter.OpLessEq: " <= ",
	},
	arg: dateArg,
}

func (r jsonbRange) compile(op crmfilter.Operator, column, key string, vals []string) (string, []interface{}, error) {
	value := "(CASE WHEN pg_input_is_valid(" + column + " ->> ?::text, '" + r.cast + "') THEN (" + column + " ->> ?::text)::" + r.cast + " END)"
	return r.compileOn(op, value, []interface{}{key, key}, vals)
}

func (r jsonbRange) compileOn(op crmfilter.Operator, value string, valueArgs []interface{}, vals []string) (string, []interface{}, error) {
	placeholder := "?::" + r.cast
	if op == crmfilter.OpBetween {
		lo, err := r.arg(vals[0])
		if err != nil {
			return "", nil, err
		}
		hi, err := r.arg(vals[1])
		if err != nil {
			return "", nil, err
		}
		return value + " BETWEEN " + placeholder + " AND " + placeholder, append(append([]interface{}{}, valueArgs...), lo, hi), nil
	}
	comparison, ok := r.comparisons[op]
	if !ok {
		return "", nil, unsupportedCustom(op, r.kind)
	}
	bound, err := r.arg(vals[0])
	if err != nil {
		return "", nil, err
	}
	sql := value + comparison + placeholder
	if op == crmfilter.OpNotEquals {
		sql = negate(sql)
	}
	return sql, append(append([]interface{}{}, valueArgs...), bound), nil
}

func compileJSONBBool(p crmfilter.Predicate, column, key string, vals []string) (string, []interface{}, error) {
	var want bool
	switch p.Operator {
	case crmfilter.OpIsTrue:
		want = true
	case crmfilter.OpIsFalse:
		want = false
	case crmfilter.OpEquals:
		parsed, err := strconv.ParseBool(vals[0])
		if err != nil {
			return "", nil, fmt.Errorf("%w: %q is not a boolean", ErrUnsupportedOperator, vals[0])
		}
		want = parsed
	default:
		return "", nil, unsupportedCustom(p.Operator, crmfilter.KindBool)
	}
	return column + " @> jsonb_build_object(?::text, ?::boolean)", []interface{}{key, want}, nil
}

func numberArg(v string) (interface{}, error) {
	return scalarArg(crmfilter.KindNumber, v)
}

func dateArg(v string) (interface{}, error) {
	t, err := crmfilter.ParseDate(v)
	if err != nil {
		return nil, err
	}
	return t.Format("2006-01-02"), nil
}

func negate(sql string) string {
	return "NOT COALESCE(" + sql + ", false)"
}

func unsupportedCustom(op crmfilter.Operator, kind crmfilter.Kind) error {
	return fmt.Errorf("%w: %q on custom kind %d", ErrUnsupportedOperator, op, kind)
}
