package crmfilter

import (
	"fmt"
	"strings"

	"vozko/domain/crmfilter"

	"github.com/lib/pq"
)

// OpportunityDescriptor maps each crmfilter.Field onto the SQL for the
// opportunities table (alias "o" by default). Unlike the conversation object,
// where owner/stage live in join tables, the opportunity's owner, carteira,
// pipeline, stage, status, source and lost_reason are plain columns, so they use
// StyleColumn with "= ANY(?)" (via compileColumn's OpIn) for set membership.
//
// value:      compared against value_cents in MINOR UNITS (cents). A predicate of
//
//	value >= 100000 means R$1.000,00. This matches the money guardrail:
//	the deal value is the customer's own sales figure, stored as a
//	plain integer, never routed through Vozko wallet money.
//
// created_at / updated_at / close_date: date columns (gte/lte/between/before/after).
// query:      free-text ILIKE over the deal title.
// custom:     a jsonb path on custom_fields keyed by Predicate.Key. The stock
//
//	ObjectDescriptor.Field(field) interface cannot carry the per-
//	predicate key, so Field(FieldCustom) reports ErrUnsupportedField
//	and callers use CompileOpportunity (below), which binds the key as
//	a positional "?" ("custom_fields->>?"). label is unsupported until
//	an opportunity_labels link table exists; the conversation-only
//	fields (channel, campaign, unread, window_open, last_activity_at)
//	are also unsupported.
type OpportunityDescriptor struct {
	// Alias is the alias of the opportunities row in the surrounding query
	// (default "o").
	Alias string
}

// NewOpportunityDescriptor returns the descriptor for the opportunity board/list
// query (alias "o").
func NewOpportunityDescriptor() OpportunityDescriptor {
	return OpportunityDescriptor{Alias: "o"}
}

// Object implements ObjectDescriptor.
func (d OpportunityDescriptor) Object() string { return "opportunity" }

func (d OpportunityDescriptor) alias() string {
	if d.Alias == "" {
		return "o"
	}
	return d.Alias
}

// Field implements ObjectDescriptor.
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
		// custom (needs the key-aware CompileOpportunity pass), label (no
		// opportunity_labels link table yet), and the conversation-only fields
		// (channel, campaign, unread, window_open, last_activity_at).
		return FieldMapping{}, fmt.Errorf("%w: %q on %s", ErrUnsupportedField, field, d.Object())
	}
}

// CompileOpportunity compiles a validated Filter for the opportunities table. It
// is the opportunity counterpart to Compile: standard predicates go through the
// shared per-operator compilers (via the OpportunityDescriptor), and custom-field
// predicates are compiled against the custom_fields jsonb column with the key
// bound as a positional "?" ("custom_fields->>?"), which the field-only
// ObjectDescriptor interface cannot express. The group/AND-OR nesting mirrors
// Compile exactly: groups combine with AND, predicates within a group per
// group.Conj() (default OR); an empty filter yields an empty clause.
func CompileOpportunity(filter crmfilter.Filter, desc OpportunityDescriptor, argStart int) (whereSQL string, args []interface{}, err error) {
	_ = argStart // not used by the "?" dialect; retained per the Compile signature

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

// compileOpportunityCustom compiles a FieldCustom predicate into a jsonb-path
// fragment on custom_fields. The key is bound as the first positional "?"
// (custom_fields->>?), so no key text is ever inlined into the SQL (no injection
// surface). Equality/contains/in operate on the text projection; range operators
// cast the projection to numeric.
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
