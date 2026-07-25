package crmfilter

import (
	"fmt"
	"strconv"
	"strings"

	"vozko/domain/crmfilter"

	"github.com/lib/pq"
)

// Compile turns a validated domain Filter into a parameterized Postgres WHERE
// fragment for the given object.
//
//   - Groups combine with AND; predicates within a group combine per
//     group.Conj() (default OR).
//   - Each predicate fragment is parenthesised; when there is more than one
//     group each group is parenthesised too, so an OR group never leaks across
//     an AND boundary.
//   - An empty filter compiles to an empty clause with no error and no args.
//   - Placeholders are GORM positional "?" (see package doc). argStart is
//     accepted for signature/forward compatibility but does not affect "?"
//     output; append the returned args positionally after any base args.
//
// The returned whereSQL is a boolean expression WITHOUT a leading "WHERE"; the
// caller AND-joins it into its own WHERE (or wraps it) exactly like the
// existing repositories join their []condition slices.
func Compile(filter crmfilter.Filter, desc ObjectDescriptor, argStart int) (whereSQL string, args []interface{}, err error) {
	_ = argStart // not used by the "?" dialect; retained per the requested signature

	if err := filter.Validate(); err != nil {
		return "", nil, err
	}
	if filter.IsEmpty() {
		return "", nil, nil
	}

	type groupClause struct {
		sql   string
		multi bool // more than one predicate -> needs wrapping under the top-level AND
	}
	var groups []groupClause
	for gi := range filter.Groups {
		g := filter.Groups[gi]
		if len(g.Predicates) == 0 {
			continue
		}
		var predSQLs []string
		for pi := range g.Predicates {
			frag, fargs, ferr := compilePredicate(g.Predicates[pi], desc)
			if ferr != nil {
				return "", nil, fmt.Errorf("group %d predicate %d: %w", gi, pi, ferr)
			}
			predSQLs = append(predSQLs, "("+frag+")")
			args = append(args, fargs...)
		}
		if len(predSQLs) == 0 {
			continue
		}
		joiner := " OR "
		if g.Conj() == crmfilter.And {
			joiner = " AND "
		}
		groups = append(groups, groupClause{sql: strings.Join(predSQLs, joiner), multi: len(predSQLs) > 1})
	}

	switch len(groups) {
	case 0:
		return "", nil, nil
	case 1:
		// A single group is the whole WHERE; its inner joins already bind.
		return groups[0].sql, args, nil
	default:
		parts := make([]string, len(groups))
		for i, gc := range groups {
			if gc.multi {
				parts[i] = "(" + gc.sql + ")"
			} else {
				parts[i] = gc.sql
			}
		}
		return strings.Join(parts, " AND "), args, nil
	}
}

func compilePredicate(p crmfilter.Predicate, desc ObjectDescriptor) (string, []interface{}, error) {
	m, err := desc.Field(p.Field)
	if err != nil {
		return "", nil, err
	}
	switch m.Style {
	case StyleColumn:
		return compileColumn(m, p)
	case StyleMembership:
		return compileMembership(m, p)
	case StyleExists:
		return compileExists(m, p)
	case StyleBool:
		return compileBool(m, p)
	case StyleText:
		return compileText(m, p)
	default:
		return "", nil, fmt.Errorf("%w: unknown mapping style", ErrUnsupportedOperator)
	}
}

func compileColumn(m FieldMapping, p crmfilter.Predicate) (string, []interface{}, error) {
	e := m.Expr
	vals := trimmedValues(p.Values)
	switch p.Operator {
	case crmfilter.OpEquals:
		a, err := scalarArg(m.Kind, vals[0])
		return e + " = ?", []interface{}{a}, err
	case crmfilter.OpNotEquals:
		// IS DISTINCT FROM keeps NULL rows visible, matching the existing
		// conversation_status default (IS DISTINCT FROM 'finished').
		a, err := scalarArg(m.Kind, vals[0])
		return e + " IS DISTINCT FROM ?", []interface{}{a}, err
	case crmfilter.OpIn:
		return e + " = ANY(?)", []interface{}{pq.Array(vals)}, nil
	case crmfilter.OpNotIn:
		return e + " <> ALL(?)", []interface{}{pq.Array(vals)}, nil
	case crmfilter.OpContains:
		return e + " ILIKE ?", []interface{}{"%" + vals[0] + "%"}, nil
	case crmfilter.OpGreaterEq:
		a, err := scalarArg(m.Kind, vals[0])
		return e + " >= ?", []interface{}{a}, err
	case crmfilter.OpLessEq:
		a, err := scalarArg(m.Kind, vals[0])
		return e + " <= ?", []interface{}{a}, err
	case crmfilter.OpAfter:
		a, err := scalarArg(m.Kind, vals[0])
		return e + " > ?", []interface{}{a}, err
	case crmfilter.OpBefore:
		a, err := scalarArg(m.Kind, vals[0])
		return e + " < ?", []interface{}{a}, err
	case crmfilter.OpBetween:
		lo, err := scalarArg(m.Kind, vals[0])
		if err != nil {
			return "", nil, err
		}
		hi, err := scalarArg(m.Kind, vals[1])
		if err != nil {
			return "", nil, err
		}
		return e + " BETWEEN ? AND ?", []interface{}{lo, hi}, nil
	case crmfilter.OpIsSet:
		return e + " IS NOT NULL", nil, nil
	case crmfilter.OpIsEmpty:
		return e + " IS NULL", nil, nil
	case crmfilter.OpIsTrue:
		return e + " = true", nil, nil
	case crmfilter.OpIsFalse:
		return e + " = false", nil, nil
	default:
		return "", nil, fmt.Errorf("%w: %q (column)", ErrUnsupportedOperator, p.Operator)
	}
}

func compileMembership(m FieldMapping, p crmfilter.Predicate) (string, []interface{}, error) {
	presence := func(negate bool) string {
		where := ""
		if m.Extra != "" {
			where = " WHERE " + m.Extra
		}
		sub := "SELECT " + m.Select + " FROM " + m.From + where
		op := " IN ("
		if negate {
			op = " NOT IN ("
		}
		return m.Subject + op + sub + ")"
	}
	match := func(negate bool) string {
		where := m.Match + " = ANY(?)"
		if m.Extra != "" {
			where += " AND " + m.Extra
		}
		sub := "SELECT " + m.Select + " FROM " + m.From + " WHERE " + where
		op := " IN ("
		if negate {
			op = " NOT IN ("
		}
		return m.Subject + op + sub + ")"
	}
	vals := trimmedValues(p.Values)
	// Extra (e.g. "workspace_id = ?") binds after the "= ANY(?)" id set, in the
	// left-to-right order the placeholders appear in the emitted subquery.
	switch p.Operator {
	case crmfilter.OpEquals, crmfilter.OpIn:
		return match(false), append([]interface{}{pq.Array(vals)}, m.ExtraArgs...), nil
	case crmfilter.OpNotEquals, crmfilter.OpNotIn:
		return match(true), append([]interface{}{pq.Array(vals)}, m.ExtraArgs...), nil
	case crmfilter.OpIsSet:
		return presence(false), m.ExtraArgs, nil
	case crmfilter.OpIsEmpty:
		return presence(true), m.ExtraArgs, nil
	default:
		return "", nil, fmt.Errorf("%w: %q (membership)", ErrUnsupportedOperator, p.Operator)
	}
}

func compileExists(m FieldMapping, p crmfilter.Predicate) (string, []interface{}, error) {
	build := func(negate, withMatch bool) string {
		conds := []string{m.Corr}
		if withMatch {
			conds = append(conds, m.Match+" = ANY(?)")
		}
		if m.Extra != "" {
			conds = append(conds, m.Extra)
		}
		kw := "EXISTS"
		if negate {
			kw = "NOT EXISTS"
		}
		return kw + " (SELECT 1 FROM " + m.From + " WHERE " + strings.Join(conds, " AND ") + ")"
	}
	vals := trimmedValues(p.Values)
	switch p.Operator {
	case crmfilter.OpEquals, crmfilter.OpIn:
		return build(false, true), append([]interface{}{pq.Array(vals)}, m.ExtraArgs...), nil
	case crmfilter.OpNotEquals, crmfilter.OpNotIn:
		return build(true, true), append([]interface{}{pq.Array(vals)}, m.ExtraArgs...), nil
	case crmfilter.OpIsSet:
		return build(false, false), m.ExtraArgs, nil
	case crmfilter.OpIsEmpty:
		return build(true, false), m.ExtraArgs, nil
	default:
		return "", nil, fmt.Errorf("%w: %q (exists)", ErrUnsupportedOperator, p.Operator)
	}
}

func compileBool(m FieldMapping, p crmfilter.Predicate) (string, []interface{}, error) {
	truthy := func() (bool, error) {
		switch p.Operator {
		case crmfilter.OpIsTrue:
			return true, nil
		case crmfilter.OpIsFalse:
			return false, nil
		case crmfilter.OpEquals:
			b, err := strconv.ParseBool(strings.TrimSpace(trimmedValues(p.Values)[0]))
			return b, err
		default:
			return false, fmt.Errorf("%w: %q (bool)", ErrUnsupportedOperator, p.Operator)
		}
	}
	t, err := truthy()
	if err != nil {
		return "", nil, err
	}
	if t {
		return m.TrueExpr, nil, nil
	}
	return m.FalseExpr, nil, nil
}

func compileText(m FieldMapping, p crmfilter.Predicate) (string, []interface{}, error) {
	if p.Operator != crmfilter.OpContains {
		return "", nil, fmt.Errorf("%w: %q (text)", ErrUnsupportedOperator, p.Operator)
	}
	pattern := "%" + trimmedValues(p.Values)[0] + "%"
	args := make([]interface{}, m.Params)
	for i := range args {
		args[i] = pattern
	}
	return m.Template, args, nil
}

// scalarArg parses one predicate value into a typed argument per the field kind.
// The filter has already been validated, so parse errors here are defensive.
func scalarArg(kind crmfilter.Kind, v string) (interface{}, error) {
	s := strings.TrimSpace(v)
	switch kind {
	case crmfilter.KindNumber:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", crmfilter.ErrInvalidNumber, v)
		}
		return f, nil
	case crmfilter.KindDate:
		t, err := crmfilter.ParseDate(s)
		if err != nil {
			return nil, err
		}
		return t, nil
	default:
		return s, nil
	}
}

// trimmedValues drops blank/whitespace-only values, matching the domain
// validator's nonEmpty semantics.
func trimmedValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}
