package crmfilter

import (
	"fmt"
	"strings"

	"github.com/lib/pq"

	"vozko/domain/crmfilter"
	"vozko/domain/lead"
)

func GeoPlacementCondition(alias string, p crmfilter.GeoPlacement) (string, []interface{}, error) {
	col := func(name string) string { return alias + "." + name }
	located := col("latitude") + " IS NOT NULL AND "
	unlocated := col("lead_id") + " IS NOT NULL AND " + col("latitude") + " IS NULL AND "
	switch p {
	case crmfilter.PlacementOnMap:
		return located + col("geo_precision") + " = ANY(?)", []interface{}{PinningPrecisions()}, nil
	case crmfilter.PlacementApproximate:
		return located + "NOT COALESCE(" + col("geo_precision") + " = ANY(?), false)", []interface{}{PinningPrecisions()}, nil
	case crmfilter.PlacementWithoutAddress:
		return col("lead_id") + " IS NULL", nil, nil
	case crmfilter.PlacementNotFound:
		return unlocated + col("geo_status") + " = ANY(?)", []interface{}{GeoStatusArray(lead.UnlocatedGeoStatuses())}, nil
	case crmfilter.PlacementQuotaExceeded:
		return unlocated + col("geo_status") + " = ?", []interface{}{string(lead.GeoQuotaExceeded)}, nil
	case crmfilter.PlacementRefused:
		return unlocated + col("geo_status") + " = ?", []interface{}{string(lead.GeoRefused)}, nil
	case crmfilter.PlacementPending:
		return unlocated + "NOT COALESCE(" + col("geo_status") + " = ANY(?), false)", []interface{}{GeoStatusArray(lead.SettledGeoStatuses())}, nil
	}
	return "", nil, fmt.Errorf("%w: %q is not a geo placement", crmfilter.ErrInvalidValue, p)
}

func GeoStatusArray(statuses []lead.GeoStatus) pq.StringArray {
	out := make(pq.StringArray, len(statuses))
	for i, s := range statuses {
		out[i] = string(s)
	}
	return out
}

func (d LeadDescriptor) compileGeoPlacement(p crmfilter.Predicate) (string, []interface{}, error) {
	var negated bool
	switch p.Operator {
	case crmfilter.OpIn, crmfilter.OpEquals:
	case crmfilter.OpNotIn, crmfilter.OpNotEquals:
		negated = true
	default:
		return "", nil, unsupported(p)
	}
	wanted := map[crmfilter.GeoPlacement]bool{}
	for _, v := range trimmedValues(p.Values) {
		placement := crmfilter.GeoPlacement(v)
		if !placement.Valid() {
			return "", nil, fmt.Errorf("%w: %q is not a geo placement", crmfilter.ErrInvalidValue, v)
		}
		wanted[placement] = true
	}
	if len(wanted) == 0 {
		return "", nil, crmfilter.ErrMissingValue
	}
	var parts, conditions []string
	var args, conditionArgs []interface{}
	for _, placement := range crmfilter.GeoPlacements() {
		if !wanted[placement] || placement == crmfilter.PlacementWithoutAddress {
			continue
		}
		condition, placementArgs, err := GeoPlacementCondition("la_f", placement)
		if err != nil {
			return "", nil, err
		}
		conditions = append(conditions, "("+condition+")")
		conditionArgs = append(conditionArgs, placementArgs...)
	}
	if len(conditions) > 0 {
		sql, membershipArgs, err := d.primaryAddressIn(false, " AND ("+strings.Join(conditions, " OR ")+")", conditionArgs...)
		if err != nil {
			return "", nil, err
		}
		parts, args = append(parts, sql), append(args, membershipArgs...)
	}
	if wanted[crmfilter.PlacementWithoutAddress] {
		sql, membershipArgs, err := d.primaryAddressIn(true, "")
		if err != nil {
			return "", nil, err
		}
		parts, args = append(parts, sql), append(args, membershipArgs...)
	}
	sql := parts[0]
	if len(parts) > 1 {
		sql = "(" + strings.Join(parts, " OR ") + ")"
	}
	if negated {
		sql = negate(sql)
	}
	return sql, args, nil
}
