package crmfilter

import (
	"fmt"
	"strings"

	"github.com/lib/pq"

	"vozko/domain/cep"
	"vozko/domain/crmfilter"
	"vozko/domain/lead"
	"vozko/infra/database"
)

var ErrWorkspaceScopeRequired = fmt.Errorf("%w: this lead field reads workspace tables and needs the workspace", crmfilter.ErrNotApplicable)

func (d LeadDescriptor) CustomFieldsColumn() string { return d.alias() + ".custom_fields" }

func (d LeadDescriptor) recordField(field crmfilter.Field) (FieldMapping, error) {
	col := func(name string) string { return d.alias() + "." + name }
	compiled := func(kind crmfilter.Kind, fn func(crmfilter.Predicate) (string, []interface{}, error)) (FieldMapping, error) {
		return FieldMapping{Style: StyleCompiled, Kind: kind, Compile: fn}, nil
	}
	flag := func(expr string) (FieldMapping, error) {
		return FieldMapping{Style: StyleBool, Kind: crmfilter.KindBool, TrueExpr: expr + " IS NOT NULL", FalseExpr: expr + " IS NULL"}, nil
	}
	address := func(column string, normalize func(string) (string, error)) (FieldMapping, error) {
		return compiled(crmfilter.KindIDSet, func(p crmfilter.Predicate) (string, []interface{}, error) {
			return d.primaryAddressColumn(p, column, normalize)
		})
	}

	switch field {
	case crmfilter.FieldID:
		return compiled(crmfilter.KindIDSet, d.compileID)
	case crmfilter.FieldPhoneAny:
		return compiled(crmfilter.KindString, d.compilePhoneAny)
	case crmfilter.FieldEmail:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindString, Expr: col("email")}, nil
	case crmfilter.FieldNickname:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindText, Expr: col("nickname")}, nil
	case crmfilter.FieldBirthday:
		return compiled(crmfilter.KindEnum, d.compileBirthday)
	case crmfilter.FieldBirthDate:
		return compiled(crmfilter.KindDate, d.compileBirthDate)
	case crmfilter.FieldOwner:
		return compiled(crmfilter.KindIDSet, d.compileOwner)
	case crmfilter.FieldSource:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindEnum, Expr: col("source")}, nil
	case crmfilter.FieldZip:
		return address("zip_code", cep.Parse)
	case crmfilter.FieldState:
		return address("state", upper)
	case crmfilter.FieldCity:
		return address("city_key", crmfilter.ParseCityKey)
	case crmfilter.FieldGeoPrecision:
		return address("geo_precision", trimmed)
	case crmfilter.FieldGeoStatus:
		return address("geo_status", trimmed)
	case crmfilter.FieldDistrict:
		return compiled(crmfilter.KindIDSet, d.compileDistrict)
	case crmfilter.FieldHasAddress:
		return compiled(crmfilter.KindBool, d.compileHasAddress)
	case crmfilter.FieldHasIdentity:
		return flag("NULLIF(" + col("number") + ", '')")
	case crmfilter.FieldOptedOut:
		return flag(col("opted_out_at"))
	case crmfilter.FieldWhatsAppOptIn:
		return flag(col("whatsapp_opt_in_at"))
	case crmfilter.FieldRelationKind:
		return compiled(crmfilter.KindEnum, d.compileRelationKind)
	case crmfilter.FieldRelativesCount:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindNumber, Expr: col("relatives_count")}, nil
	case crmfilter.FieldReferredCount:
		return FieldMapping{Style: StyleColumn, Kind: crmfilter.KindNumber, Expr: col("referred_count")}, nil
	case crmfilter.FieldReferredBy:
		return compiled(crmfilter.KindIDSet, d.compileReferredBy)
	case crmfilter.FieldGeoPlacement:
		return compiled(crmfilter.KindEnum, d.compileGeoPlacement)
	case crmfilter.FieldAreaApproximate:
		return d.areaField()
	}
	return FieldMapping{}, fmt.Errorf("%w: %q on %s", ErrUnsupportedField, field, d.Object())
}

func (d LeadDescriptor) workspace() (string, error) {
	ws := strings.TrimSpace(d.WorkspaceID)
	if ws == "" {
		return "", ErrWorkspaceScopeRequired
	}
	return ws, nil
}

func (d LeadDescriptor) compileID(p crmfilter.Predicate) (string, []interface{}, error) {
	ids := pq.Array(trimmedValues(p.Values))
	switch p.Operator {
	case crmfilter.OpEquals, crmfilter.OpIn:
		return d.id() + " = ANY(?::uuid[])", []interface{}{ids}, nil
	case crmfilter.OpNotEquals, crmfilter.OpNotIn:
		return d.id() + " <> ALL(?::uuid[])", []interface{}{ids}, nil
	}
	return "", nil, unsupported(p)
}

func (d LeadDescriptor) phoneMatch(alias, comparison string, value interface{}) (string, []interface{}, error) {
	ws, err := d.workspace()
	if err != nil {
		return "", nil, err
	}
	sql := "(" + d.alias() + ".number " + comparison + " OR " + d.id() + " IN (SELECT " + alias + ".lead_id FROM lead_phones " + alias +
		" WHERE " + alias + ".workspace_id = ? AND " + alias + ".number " + comparison + "))"
	return sql, []interface{}{value, ws, value}, nil
}

func (d LeadDescriptor) compilePhoneAny(p crmfilter.Predicate) (string, []interface{}, error) {
	numbers, err := crmfilter.PhoneAnyNumbers(p.Values)
	if err != nil {
		return "", nil, err
	}
	match, args, err := d.phoneMatch("lp_f", "= ANY(?)", pq.Array(numbers))
	if err != nil {
		return "", nil, err
	}
	switch p.Operator {
	case crmfilter.OpEquals, crmfilter.OpIn:
		return match, args, nil
	case crmfilter.OpNotEquals, crmfilter.OpNotIn:
		return negate(match), args, nil
	}
	return "", nil, unsupported(p)
}

func (d LeadDescriptor) compileNumber(p crmfilter.Predicate) (string, []interface{}, error) {
	identity := "NULLIF(" + d.alias() + ".number, '')"
	vals := trimmedValues(p.Values)
	switch p.Operator {
	case crmfilter.OpIsSet:
		return identity + " IS NOT NULL", nil, nil
	case crmfilter.OpIsEmpty:
		return identity + " IS NULL", nil, nil
	case crmfilter.OpContains:
		return d.phoneMatch("lp_f", "LIKE ?", database.LikeContains(vals[0]))
	case crmfilter.OpEquals, crmfilter.OpIn:
		return d.phoneMatch("lp_f", "= ANY(?)", pq.Array(vals))
	case crmfilter.OpNotEquals, crmfilter.OpNotIn:
		match, args, err := d.phoneMatch("lp_f", "= ANY(?)", pq.Array(vals))
		return negate(match), args, err
	}
	return "", nil, unsupported(p)
}

func (d LeadDescriptor) birthdayExpr() string {
	birth := d.alias() + ".birth_date"
	return "(extract(month from " + birth + "), extract(day from " + birth + "))"
}

func (d LeadDescriptor) compileBirthday(p crmfilter.Predicate) (string, []interface{}, error) {
	if p.Operator != crmfilter.OpEquals && p.Operator != crmfilter.OpIn {
		return "", nil, unsupported(p)
	}
	ranges, err := crmfilter.BirthdayRanges(p.Values, d.Today)
	if err != nil {
		return "", nil, err
	}
	expr := d.birthdayExpr()
	clauses := make([]string, 0, len(ranges))
	args := make([]interface{}, 0, 4*len(ranges))
	for _, r := range ranges {
		clauses = append(clauses, "("+expr+" >= (?, ?) AND "+expr+" <= (?, ?))")
		args = append(args, r.From.Month, r.From.Day, r.To.Month, r.To.Day)
	}
	return d.alias() + ".birth_date IS NOT NULL AND (" + strings.Join(clauses, " OR ") + ")", args, nil
}

func (d LeadDescriptor) compileBirthDate(p crmfilter.Predicate) (string, []interface{}, error) {
	col := d.alias() + ".birth_date"
	switch p.Operator {
	case crmfilter.OpIsSet:
		return col + " IS NOT NULL", nil, nil
	case crmfilter.OpIsEmpty:
		return col + " IS NULL", nil, nil
	}
	return dateRange.compileOn(p.Operator, col, nil, trimmedValues(p.Values))
}

func (d LeadDescriptor) compileOwner(p crmfilter.Predicate) (string, []interface{}, error) {
	owner := d.alias() + ".owner_id"
	ids, kinds := lead.OwnerColumns(p.Values)
	pairs := "(" + owner + ", " + d.alias() + ".owner_kind) IN (SELECT * FROM unnest(?::uuid[], ?::text[]))"
	args := []interface{}{pq.Array(ids), pq.Array(kinds)}
	switch p.Operator {
	case crmfilter.OpIsSet:
		return owner + " IS NOT NULL", nil, nil
	case crmfilter.OpIsEmpty:
		return owner + " IS NULL", nil, nil
	case crmfilter.OpEquals, crmfilter.OpIn:
		return pairs, args, nil
	case crmfilter.OpNotEquals, crmfilter.OpNotIn:
		return negate(pairs), args, nil
	}
	return "", nil, unsupported(p)
}

func (d LeadDescriptor) primaryAddressIn(negated bool, condition string, args ...interface{}) (string, []interface{}, error) {
	ws, err := d.workspace()
	if err != nil {
		return "", nil, err
	}
	membership := " IN ("
	if negated {
		membership = " NOT IN ("
	}
	sql := d.id() + membership + "SELECT la_f.lead_id FROM lead_addresses la_f WHERE la_f.workspace_id = ? AND la_f.is_primary" + condition + ")"
	return sql, append([]interface{}{ws}, args...), nil
}

func (d LeadDescriptor) primaryAddressColumn(p crmfilter.Predicate, column string, normalize func(string) (string, error)) (string, []interface{}, error) {
	qualified := "la_f." + column
	switch p.Operator {
	case crmfilter.OpIsSet, crmfilter.OpIsEmpty:
		return d.primaryAddressIn(p.Operator == crmfilter.OpIsEmpty, " AND "+qualified+" IS NOT NULL")
	case crmfilter.OpEquals, crmfilter.OpIn, crmfilter.OpNotEquals, crmfilter.OpNotIn:
		values, err := normalizeAll(trimmedValues(p.Values), normalize)
		if err != nil {
			return "", nil, err
		}
		negated := p.Operator == crmfilter.OpNotEquals || p.Operator == crmfilter.OpNotIn
		return d.primaryAddressIn(negated, " AND "+qualified+" = ANY(?)", pq.Array(values))
	}
	return "", nil, unsupported(p)
}

func (d LeadDescriptor) compileDistrict(p crmfilter.Predicate) (string, []interface{}, error) {
	switch p.Operator {
	case crmfilter.OpIsSet, crmfilter.OpIsEmpty:
		return d.primaryAddressIn(p.Operator == crmfilter.OpIsEmpty, " AND la_f.district_key IS NOT NULL")
	case crmfilter.OpEquals, crmfilter.OpIn, crmfilter.OpNotEquals, crmfilter.OpNotIn:
	default:
		return "", nil, unsupported(p)
	}
	values := trimmedValues(p.Values)
	cities, districts := make([]string, 0, len(values)), make([]string, 0, len(values))
	for _, v := range values {
		cityKey, districtKey, err := crmfilter.ParseDistrictPair(v)
		if err != nil {
			return "", nil, err
		}
		cities, districts = append(cities, cityKey), append(districts, districtKey)
	}
	negated := p.Operator == crmfilter.OpNotEquals || p.Operator == crmfilter.OpNotIn
	return d.primaryAddressIn(negated, " AND (la_f.city_key, la_f.district_key) IN (SELECT * FROM unnest(?::text[], ?::text[]))", pq.Array(cities), pq.Array(districts))
}

func (d LeadDescriptor) compileHasAddress(p crmfilter.Predicate) (string, []interface{}, error) {
	present, err := boolOperand(p)
	if err != nil {
		return "", nil, err
	}
	return d.primaryAddressIn(!present, "")
}

func (d LeadDescriptor) compileRelationKind(p crmfilter.Predicate) (string, []interface{}, error) {
	ws, err := d.workspace()
	if err != nil {
		return "", nil, err
	}
	union := func(kindCondition func(string) string, kinds, inverses interface{}) (string, []interface{}) {
		sql := "SELECT lr_k.lead_id FROM lead_relations lr_k WHERE lr_k.workspace_id = ?" + kindCondition("lr_k") +
			" UNION ALL SELECT lr_i.other_lead_id FROM lead_relations lr_i WHERE lr_i.workspace_id = ?" + kindCondition("lr_i")
		if kinds == nil {
			return sql, []interface{}{ws, ws}
		}
		return sql, []interface{}{ws, kinds, ws, inverses}
	}
	membership := func(negated bool, sub string, args []interface{}) (string, []interface{}, error) {
		op := " IN ("
		if negated {
			op = " NOT IN ("
		}
		return d.id() + op + sub + ")", args, nil
	}
	switch p.Operator {
	case crmfilter.OpIsSet, crmfilter.OpIsEmpty:
		sub, args := union(func(string) string { return "" }, nil, nil)
		return membership(p.Operator == crmfilter.OpIsEmpty, sub, args)
	case crmfilter.OpEquals, crmfilter.OpIn, crmfilter.OpNotEquals, crmfilter.OpNotIn:
		kinds := trimmedValues(p.Values)
		inverses := make([]string, 0, len(kinds))
		for _, k := range kinds {
			inverses = append(inverses, string(lead.RelationKind(k).Inverse()))
		}
		sub, args := union(func(alias string) string { return " AND " + alias + ".kind = ANY(?)" }, pq.Array(kinds), pq.Array(inverses))
		return membership(p.Operator == crmfilter.OpNotEquals || p.Operator == crmfilter.OpNotIn, sub, args)
	}
	return "", nil, unsupported(p)
}

func (d LeadDescriptor) compileReferredBy(p crmfilter.Predicate) (string, []interface{}, error) {
	ws, err := d.workspace()
	if err != nil {
		return "", nil, err
	}
	sub := "SELECT lr_b.other_lead_id FROM lead_relations lr_b WHERE lr_b.workspace_id = ? AND lr_b.kind = ? AND lr_b.lead_id = ANY(?::uuid[])"
	args := []interface{}{ws, string(lead.KindReferred), pq.Array(trimmedValues(p.Values))}
	switch p.Operator {
	case crmfilter.OpEquals, crmfilter.OpIn:
		return d.id() + " IN (" + sub + ")", args, nil
	case crmfilter.OpNotEquals, crmfilter.OpNotIn:
		return d.id() + " NOT IN (" + sub + ")", args, nil
	}
	return "", nil, unsupported(p)
}

func boolOperand(p crmfilter.Predicate) (bool, error) {
	switch p.Operator {
	case crmfilter.OpIsTrue:
		return true, nil
	case crmfilter.OpIsFalse:
		return false, nil
	case crmfilter.OpEquals:
		switch strings.ToLower(trimmedValues(p.Values)[0]) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, unsupported(p)
}

func normalizeAll(values []string, normalize func(string) (string, error)) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		n, err := normalize(v)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", crmfilter.ErrInvalidValue, v)
		}
		out = append(out, n)
	}
	return out, nil
}

func upper(v string) (string, error) { return strings.ToUpper(strings.TrimSpace(v)), nil }

func trimmed(v string) (string, error) { return strings.TrimSpace(v), nil }

func unsupported(p crmfilter.Predicate) error {
	return fmt.Errorf("%w: %q on %q", ErrUnsupportedOperator, p.Operator, p.Field)
}

func (d LeadDescriptor) BirthdayCondition(windows ...string) (string, []interface{}, error) {
	return d.compileBirthday(crmfilter.Predicate{Field: crmfilter.FieldBirthday, Operator: crmfilter.OpIn, Values: windows})
}
