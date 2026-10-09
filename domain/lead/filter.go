package lead

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/actor"
	"vozko/domain/crmfilter"
	"vozko/domain/geo"
)

var ErrLeadFilterAddressForbidden = errors.New("lead: filtering by CEP, map precision, map status, map placement or area requires permission to read full addresses")

var addressFilterFields = []crmfilter.Field{
	crmfilter.FieldZip,
	crmfilter.FieldGeoPrecision,
	crmfilter.FieldGeoStatus,
	crmfilter.FieldArea,
	crmfilter.FieldAreaApproximate,
	crmfilter.FieldGeoPlacement,
}

func CheckAddressFilter(f crmfilter.Filter, v Viewer) error {
	if v.ReadsAddresses {
		return nil
	}
	for _, field := range addressFilterFields {
		if f.UsesField(field) {
			return fmt.Errorf("%w: %q", ErrLeadFilterAddressForbidden, field)
		}
	}
	return nil
}

var leadValueRules = map[crmfilter.Field]func(string) bool{
	crmfilter.FieldOwner:        ownerValue,
	crmfilter.FieldSource:       func(v string) bool { return Source(v).Valid() },
	crmfilter.FieldGeoStatus:    func(v string) bool { return GeoStatus(v).Valid() },
	crmfilter.FieldGeoPrecision: func(v string) bool { return geo.Precision(v).Known() },
	crmfilter.FieldRelationKind: func(v string) bool { return RelationKind(v).Valid() },
}

func ValidateFilter(f crmfilter.Filter) error {
	if err := f.Validate(); err != nil {
		return err
	}
	for gi, g := range f.Groups {
		for pi, p := range g.Predicates {
			if err := validSearch(p); err != nil {
				return fmt.Errorf("group %d predicate %d: %w", gi, pi, err)
			}
			rule, ok := leadValueRules[p.Field]
			if !ok {
				continue
			}
			for _, v := range p.Values {
				v = strings.TrimSpace(v)
				if v == "" || rule(v) {
					continue
				}
				return fmt.Errorf("group %d predicate %d: %w: %q on %q", gi, pi, crmfilter.ErrInvalidValue, v, p.Field)
			}
		}
	}
	return nil
}

func validSearch(p crmfilter.Predicate) error {
	if p.Field != crmfilter.FieldQuery {
		return nil
	}
	for _, v := range p.Values {
		if _, err := ParseSearch(v); err != nil {
			return err
		}
	}
	return nil
}

func ownerValue(v string) bool {
	id, _ := actor.Split(v)
	return v != "" && validOwner(v) && isUUID(id)
}

func isUUID(v string) bool {
	parsed, err := uuid.Parse(v)
	return err == nil && parsed.String() == strings.ToLower(v)
}

func OwnerColumns(values []string) (ids, kinds []string) {
	ids, kinds = make([]string, 0, len(values)), make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v == "" {
			continue
		}
		id, kind := actor.Split(v)
		ids, kinds = append(ids, id), append(kinds, string(kind))
	}
	return ids, kinds
}
