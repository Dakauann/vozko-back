package geo

import (
	"context"
	"maps"
	"strings"
	"time"

	"vozko/domain/address"
	"vozko/domain/cep"
)

type UnavailableReason string

const (
	ReasonReferenceNotLoaded UnavailableReason = "reference_not_loaded"
	ReasonReferenceDown      UnavailableReason = "reference_unavailable"
)

type CityName struct {
	State   string
	NameKey string
}

func CityNameOf(p address.Postal) (CityName, bool) {
	name := CityName{State: strings.ToUpper(strings.TrimSpace(p.State)), NameKey: address.CityNameKey(p.City)}
	return name, name.State != "" && name.NameKey != ""
}

type ReferencePoint struct {
	Point    Point
	SpreadM  float64
	Count    int64
	CityCode string
	Name     string
	State    string
}

type ReferenceKeys struct {
	ZipCodes  []string
	CityNames []CityName
}

type ReferencePlaces struct {
	Districts []address.Place
	CityCodes []string
}

func (p ReferencePlaces) Empty() bool { return len(p.Districts) == 0 && len(p.CityCodes) == 0 }

type ReferenceIndex struct {
	CEPs      map[string]ReferencePoint
	Districts map[address.Place]ReferencePoint
	Cities    map[string]ReferencePoint
	CityCodes map[CityName]string
}

type Coverage struct {
	States map[string]bool
}

func (c Coverage) Full() bool {
	for _, s := range address.IBGEStates() {
		if !c.States[s.State] {
			return false
		}
	}
	return true
}

func (c Coverage) Covers(raw address.Postal) bool {
	p := raw.Normalize()
	if state, ok := address.StateCode(p.State); ok {
		return c.States[state]
	}
	if state, ok := address.StateOfCityCode(p.CityCode); ok {
		return c.States[state]
	}
	return c.Full()
}

type Reference interface {
	Coverage(ctx context.Context) (Coverage, error)
	Lookup(ctx context.Context, postals []address.Postal) (ReferenceIndex, error)
}

func ReferenceKeysFor(postals []address.Postal) ReferenceKeys {
	var keys ReferenceKeys
	zips, names := map[string]bool{}, map[CityName]bool{}
	for _, raw := range postals {
		p := raw.Normalize()
		if zip, err := cep.Parse(p.ZipCode); err == nil && !zips[zip] {
			zips[zip] = true
			keys.ZipCodes = append(keys.ZipCodes, zip)
		}
		if address.ValidCityCode(p.CityCode) {
			continue
		}
		if name, ok := CityNameOf(p); ok && !names[name] {
			names[name] = true
			keys.CityNames = append(keys.CityNames, name)
		}
	}
	return keys
}

func (idx ReferenceIndex) CityCodeOf(raw address.Postal) string {
	p := raw.Normalize()
	if address.ValidCityCode(p.CityCode) {
		return p.CityCode
	}
	if zip, err := cep.Parse(p.ZipCode); err == nil {
		if point, ok := idx.CEPs[zip]; ok && address.ValidCityCode(point.CityCode) {
			return point.CityCode
		}
	}
	if name, ok := CityNameOf(p); ok {
		return idx.CityCodes[name]
	}
	return ""
}

func (idx ReferenceIndex) PlacesFor(postals []address.Postal) ReferencePlaces {
	var places ReferencePlaces
	seenPlaces, seenCodes := map[address.Place]bool{}, map[string]bool{}
	for _, p := range postals {
		code := idx.CityCodeOf(p)
		if code == "" {
			continue
		}
		if key := address.DistrictKey(p.District); key != "" {
			place := address.Place{CityCode: code, DistrictKey: key}
			if !seenPlaces[place] {
				seenPlaces[place] = true
				places.Districts = append(places.Districts, place)
			}
		}
		if !seenCodes[code] {
			seenCodes[code] = true
			places.CityCodes = append(places.CityCodes, code)
		}
	}
	return places
}

func (idx ReferenceIndex) Merge(other ReferenceIndex) ReferenceIndex {
	return ReferenceIndex{
		CEPs:      mergedMap(idx.CEPs, other.CEPs),
		Districts: mergedMap(idx.Districts, other.Districts),
		Cities:    mergedMap(idx.Cities, other.Cities),
		CityCodes: mergedMap(idx.CityCodes, other.CityCodes),
	}
}

func mergedMap[K comparable, V any](a, b map[K]V) map[K]V {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make(map[K]V, len(a)+len(b))
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}

func (idx ReferenceIndex) Candidates(raw address.Postal, at time.Time) []Fix {
	p := raw.Normalize()
	reference := func(point Point, precision Precision) Fix {
		return Fix{Point: point, Precision: precision, Source: SourceReference, FixedAt: at}
	}
	var out []Fix
	if zip, err := cep.Parse(p.ZipCode); err == nil {
		if point, ok := idx.CEPs[zip]; ok {
			out = append(out, reference(point.Point, CEPPrecision(zip, point.SpreadM)))
		}
	}
	code := idx.CityCodeOf(p)
	if code == "" {
		return out
	}
	if key := address.DistrictKey(p.District); key != "" {
		if point, ok := idx.Districts[address.Place{CityCode: code, DistrictKey: key}]; ok {
			out = append(out, reference(point.Point, PrecisionDistrict))
		}
	}
	if point, ok := idx.Cities[code]; ok {
		out = append(out, reference(point.Point, PrecisionCity))
	}
	return out
}
