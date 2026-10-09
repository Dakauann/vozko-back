package crmfilter

import (
	"fmt"
	"strings"

	"github.com/lib/pq"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
)

var ErrUnboundArea = fmt.Errorf("%w: area predicate was not resolved against the readable areas", crmfilter.ErrNotApplicable)

const areaRing = "(la_f.latitude BETWEEN ? AND ? AND la_f.longitude BETWEEN ? AND ?" +
	" AND CASE WHEN point(la_f.longitude, la_f.latitude) <@ (SELECT ar.ring FROM lead_areas ar WHERE ar.id = ?::uuid AND ar.workspace_id = ? AND ar.deleted_at IS NULL) THEN true ELSE false END)"

func (d LeadDescriptor) areaField() (FieldMapping, error) {
	return FieldMapping{Style: StyleCompiled, Kind: crmfilter.KindIDSet, Compile: d.compileArea}, nil
}

func areaPlacementCondition(p crmfilter.Predicate) (string, []interface{}, error) {
	placements, err := crmfilter.AreaPlacements(p)
	if err != nil {
		return "", nil, err
	}
	conditions := make([]string, 0, len(placements))
	var args []interface{}
	for _, placement := range placements {
		condition, placementArgs, err := GeoPlacementCondition("la_f", placement)
		if err != nil {
			return "", nil, err
		}
		conditions = append(conditions, condition)
		args = append(args, placementArgs...)
	}
	if len(conditions) == 1 {
		return conditions[0], args, nil
	}
	return "((" + strings.Join(conditions, ") OR (") + "))", args, nil
}

func (d LeadDescriptor) compileArea(p crmfilter.Predicate) (string, []interface{}, error) {
	if _, bound := p.BoundKind(); !bound {
		return "", nil, ErrUnboundArea
	}
	if p.Operator != crmfilter.OpIn {
		return "", nil, fmt.Errorf("%w: %q on area", ErrUnsupportedOperator, p.Operator)
	}
	ws, err := d.workspace()
	if err != nil {
		return "", nil, err
	}
	ids := trimmedValues(p.Values)
	if len(ids) == 0 {
		return "", nil, crmfilter.ErrMissingValue
	}
	placed, placedArgs, err := areaPlacementCondition(p)
	if err != nil {
		return "", nil, err
	}
	bounds := map[string]crmfilter.AreaBounds{}
	for _, b := range p.BoundAreas() {
		bounds[b.ID] = b
	}
	rings := make([]string, len(ids))
	args := append([]interface{}{}, placedArgs...)
	for i, id := range ids {
		b, ok := bounds[id]
		if !ok {
			return "", nil, fmt.Errorf("%w: no bounds for %s", ErrUnboundArea, id)
		}
		rings[i] = areaRing
		args = append(args, b.South, b.North, b.West, b.East, id, ws)
	}
	return d.primaryAddressIn(false, " AND "+placed+" AND ("+strings.Join(rings, " OR ")+")", args...)
}

func PrecisionArray(precisions []geo.Precision) pq.StringArray {
	out := make(pq.StringArray, len(precisions))
	for i, p := range precisions {
		out[i] = string(p)
	}
	return out
}

func PinningPrecisions() pq.StringArray {
	return PrecisionArray(geo.PinningPrecisions())
}
