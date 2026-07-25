package shortlink

import (
	"strings"

	"vozko/domain/shortlink"
)

type edgeGeoResolver struct{}

func NewEdgeGeoResolver() shortlink.GeoResolver {
	return &edgeGeoResolver{}
}

func (r *edgeGeoResolver) Resolve(hints shortlink.GeoHints) shortlink.GeoInfo {
	return shortlink.GeoInfo{
		Country: normalizeCountry(hints.Country),
		Region:  strings.TrimSpace(hints.Region),
		City:    strings.TrimSpace(hints.City),
	}
}

func normalizeCountry(country string) string {
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "XX" || country == "T1" {
		return ""
	}
	return country
}
