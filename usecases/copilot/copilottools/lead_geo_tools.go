package copilottools

import (
	"context"

	"vozko/domain/copilot"
	"vozko/domain/lead"
	"vozko/domain/tools"
)

const geoPlacesShown = 20

type leadGeoSummaryTool struct{ deps LeadDeps }

func NewLeadGeoSummaryTool(deps LeadDeps) copilot.Tool { return &leadGeoSummaryTool{deps: deps} }

func (t *leadGeoSummaryTool) Meta() copilot.Meta { return leadReadMeta() }

func (t *leadGeoSummaryTool) Definition() tools.Definition {
	return definition("lead_geo_summary",
		"Conta os leads de um filtro pelo lugar: quantos têm endereço, estão no mapa, são aproximados, estão sem endereço, "+
			"não foram encontrados, foram recusados ou aguardam localização, e as cidades e os bairros com mais leads (com city_key e pair; more_cities e more_districts dizem se há outros lugares além dos listados), "+
			"que search_leads e prepare_lead_action aceitam). Nunca traz endereços nem números.",
		leadFilterArgs{})
}

type geoCity struct {
	CityKey string `json:"city_key"`
	City    string `json:"city"`
	State   string `json:"state"`
	Leads   int64  `json:"leads"`
}

type geoDistrict struct {
	Pair     string `json:"pair"`
	District string `json:"district"`
	City     string `json:"city"`
	State    string `json:"state"`
	Leads    int64  `json:"leads"`
}

func (t *leadGeoSummaryTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a leadFilterArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if t.deps.Sections == nil {
		return leadFailure("lead_geo_summary", errLeadToolUnavailable)
	}
	filter, err := leadFilterOf(ctx, cc, t.deps, a)
	if err != nil {
		return leadFailure("lead_geo_summary", err)
	}
	viewer := viewerOf(cc)
	summary, err := t.deps.Sections.Summary(ctx, viewer, filter)
	if err != nil {
		return leadFailure("lead_geo_summary", err)
	}
	places, err := t.deps.Sections.Places(ctx, viewer, filter)
	if err != nil {
		return leadFailure("lead_geo_summary", err)
	}
	if summary == nil || places == nil {
		return leadFailure("lead_geo_summary", errLeadToolUnavailable)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: geoSummaryData(summary, places)}
}

func geoSummaryData(s *lead.SummarySection, p *lead.PlacesSection) map[string]interface{} {
	cities := make([]geoCity, 0, min(len(p.Cities), geoPlacesShown))
	for _, c := range p.Cities[:min(len(p.Cities), geoPlacesShown)] {
		cities = append(cities, geoCity{CityKey: c.CityKey, City: c.City, State: c.State, Leads: c.Count})
	}
	districts := make([]geoDistrict, 0, min(len(p.Districts), geoPlacesShown))
	for _, d := range p.Districts[:min(len(p.Districts), geoPlacesShown)] {
		districts = append(districts, geoDistrict{Pair: d.Pair, District: d.District, City: d.City, State: d.State, Leads: d.Count})
	}
	return map[string]interface{}{
		"total":           s.Total,
		"with_address":    s.WithAddress,
		"on_map":          s.OnMap,
		"approximate":     s.Approximate,
		"without_address": s.WithoutAddress,
		"not_found":       s.NotFound,
		"pending":         s.Pending,
		"quota_exceeded":  s.QuotaExceeded,
		"refused":         s.Refused,
		"blocked":         s.Blocked,
		"cities":          cities,
		"more_cities":     len(p.Cities) > len(cities),
		"districts":       districts,
		"more_districts":  len(p.Districts) > len(districts),
	}
}
