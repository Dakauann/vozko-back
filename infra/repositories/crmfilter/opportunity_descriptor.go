package crmfilter

import (
	"fmt"
	"strings"

	"vozko/domain/crmfilter"

	"github.com/lib/pq"
)

type OpportunityDescriptor struct {
	Alias string
}

func NewOpportunityDescriptor() OpportunityDescriptor {
	return OpportunityDescriptor{Alias: "o"}
}

func (d OpportunityDescriptor) Object() string { return "opportunity" }

func (d OpportunityDescriptor) alias() string {
	if d.Alias == "" {
		return "o"
	}
	return d.Alias
}

func (d OpportunityDescriptor) Field(field crmfilter.Field) (FieldMapping, error) {
	a := d.alias()
	col := func(name string) string { return a + "." + name }

	switch field {
	case crmfilter.FieldOwner:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: col("owner_id")}, nil
	case crmfilter.FieldCarteira:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: col("carteira_id")}, nil
	case crmfilter.FieldPipeline:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: col("pipeline_id")}, nil
	case crmfilter.FieldStage:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: col("stage_id")}, nil
	case crmfilter.FieldLostReason:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindIDSet, Expr: col("lost_reason_id")}, nil

	case crmfilter.FieldStatus:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindEnum, Expr: col("status")}, nil
	case crmfilter.FieldSource:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindEnum, Expr: col("source")}, nil

	case crmfilter.FieldValue:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindNumber, Expr: col("value_cents")}, nil

	case crmfilter.FieldCreatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: col("created_at")}, nil
	case crmfilter.FieldUpdatedAt:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: col("updated_at")}, nil
	case crmfilter.FieldCloseDate:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindDate, Expr: col("close_date")}, nil

	case crmfilter.FieldQuery:
		return FieldMapping{Style: StyleText, Kind: crmfilter.KindText, Template: col("title") + " ILIKE ?", Params: 1}, nil

	default:
		return FieldMapping{}, fmt.Errorf("%w: %q on %s", ErrUnsupportedField, field, d.Object())
	}
}

func CompileOpportunity(filter crmfilter.Filter, desc OpportunityDescriptor, argStart int) (whereSQL string, args []interface{}, err error) {
	_ = argStart

	if err := filter.Validate(); err != nil {
		return "", nil, err
	}
	if filter.IsEmpty() {
		return "", nil, nil
	}

	type groupClause struct {
		sql   string
		multi bool
	}
	var groups []groupClause
	for gi := range filter.Groups {
		g := filter.Groups[gi]
		if len(g.Predicates) == 0 {
			continue
		}
		var predSQLs []string
		for pi := range g.Predicates {
			p := g.Predicates[pi]
			var frag string
			var fargs []interface{}
			var ferr error
			if p.Field == crmfilter.FieldCustom {
				frag, fargs, ferr = compileOpportunityCustom(p, desc)
			} else {
				frag, fargs, ferr = compilePredicate(p, desc)
			}
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

func compileOpportunityCustom(p crmfilter.Predicate, desc OpportunityDescriptor) (string, []interface{}, error) {
	key := strings.TrimSpace(p.Key)
	if key == "" {
		return "", nil, crmfilter.ErrMissingCustomKey
	}
	path := desc.alias() + ".custom_fields->>?"
	numPath := "(" + path + ")::numeric"
	vals := trimmedValues(p.Values)

	switch p.Operator {
	case crmfilter.OpEquals:
		return path + " = ?", []interface{}{key, vals[0]}, nil
	case crmfilter.OpNotEquals:
		return path + " IS DISTINCT FROM ?", []interface{}{key, vals[0]}, nil
	case crmfilter.OpIn:
		return path + " = ANY(?)", []interface{}{key, pq.Array(vals)}, nil
	case crmfilter.OpNotIn:
		return path + " <> ALL(?)", []interface{}{key, pq.Array(vals)}, nil
	case crmfilter.OpContains:
		return path + " ILIKE ?", []interface{}{key, "%" + vals[0] + "%"}, nil
	case crmfilter.OpGreaterEq:
		n, e := scalarArg(crmfilter.KindNumber, vals[0])
		return numPath + " >= ?", []interface{}{key, n}, e
	case crmfilter.OpLessEq:
		n, e := scalarArg(crmfilter.KindNumber, vals[0])
		return numPath + " <= ?", []interface{}{key, n}, e
	case crmfilter.OpBetween:
		lo, e := scalarArg(crmfilter.KindNumber, vals[0])
		if e != nil {
			return "", nil, e
		}
		hi, e := scalarArg(crmfilter.KindNumber, vals[1])
		if e != nil {
			return "", nil, e
		}
		return numPath + " BETWEEN ? AND ?", []interface{}{key, lo, hi}, nil
	case crmfilter.OpIsSet:
		return path + " IS NOT NULL", []interface{}{key}, nil
	case crmfilter.OpIsEmpty:
		return path + " IS NULL", []interface{}{key}, nil
	default:
		return "", nil, fmt.Errorf("%w: %q (custom)", ErrUnsupportedOperator, p.Operator)
	}
}
