package workspaceconfig

import (
	"time"

	"vozko/domain/geocoding"
)

type GeocodingPlatformResponse struct {
	CycleStart     string                               `json:"cycleStart"`
	NextCycleStart string                               `json:"nextCycleStart"`
	Today          string                               `json:"today"`
	Cycles         []string                             `json:"cycles"`
	Items          []GeocodingPlatformWorkspaceResponse `json:"items"`
	Page           int                                  `json:"page"`
	PageSize       int                                  `json:"pageSize"`
	TotalItems     int64                                `json:"totalItems"`
	TotalPages     int                                  `json:"totalPages"`
}

type GeocodingPlatformWorkspaceResponse struct {
	WorkspaceID       string                            `json:"workspaceId"`
	WorkspaceName     string                            `json:"workspaceName"`
	Provider          string                            `json:"provider"`
	Enabled           bool                              `json:"enabled"`
	MonthlyCeiling    int64                             `json:"monthlyCeiling"`
	CeilingSet        bool                              `json:"ceilingSet"`
	ProviderChangedAt *string                           `json:"providerChangedAt"`
	UsedThisCycle     int64                             `json:"usedThisCycle"`
	UsedToday         int64                             `json:"usedToday"`
	Months            []GeocodingPlatformMonthResponse  `json:"months"`
	Coverage          GeocodingPlatformCoverageResponse `json:"coverage"`
}

type GeocodingPlatformMonthResponse struct {
	CycleStart string `json:"cycleStart"`
	Requests   int64  `json:"requests"`
}

type GeocodingPlatformCoverageResponse struct {
	Total          int64   `json:"total"`
	WithAddress    int64   `json:"withAddress"`
	WithoutAddress int64   `json:"withoutAddress"`
	OnMap          int64   `json:"onMap"`
	Approximate    int64   `json:"approximate"`
	Pending        int64   `json:"pending"`
	NotFound       int64   `json:"notFound"`
	QuotaExceeded  int64   `json:"quotaExceeded"`
	Refused        int64   `json:"refused"`
	AddressShare   float64 `json:"addressShare"`
	MapShare       float64 `json:"mapShare"`
}

func utcText(at time.Time) string {
	return at.UTC().Format(time.RFC3339)
}

func toGeocodingPlatformResponse(page geocoding.PlatformPage) GeocodingPlatformResponse {
	out := GeocodingPlatformResponse{
		CycleStart:     utcText(page.Period.CycleStart),
		NextCycleStart: utcText(page.Period.NextCycle),
		Today:          utcText(page.Period.Day),
		Items:          make([]GeocodingPlatformWorkspaceResponse, 0, len(page.Items)),
		Page:           page.Page,
		PageSize:       page.PageSize,
		TotalItems:     page.TotalItems,
		TotalPages:     page.TotalPages(),
	}
	for _, cycle := range page.Period.HistoryCycles() {
		out.Cycles = append(out.Cycles, utcText(cycle))
	}
	for _, item := range page.Items {
		out.Items = append(out.Items, toGeocodingPlatformWorkspace(item))
	}
	return out
}

func toGeocodingPlatformWorkspace(item geocoding.WorkspaceGeocoding) GeocodingPlatformWorkspaceResponse {
	c := item.Coverage
	out := GeocodingPlatformWorkspaceResponse{
		WorkspaceID:       item.Workspace.ID,
		WorkspaceName:     item.Workspace.Name,
		Provider:          string(item.Settings.Provider),
		Enabled:           item.Settings.Enabled(),
		MonthlyCeiling:    item.Settings.Ceiling(),
		CeilingSet:        item.Settings.MonthlyCeiling != nil,
		ProviderChangedAt: rfc3339(item.Settings.ProviderChangedAt),
		UsedThisCycle:     item.Usage.Requests,
		UsedToday:         item.Usage.DayRequests,
		Months:            make([]GeocodingPlatformMonthResponse, 0, len(item.Months)),
		Coverage: GeocodingPlatformCoverageResponse{
			Total: c.Total, WithAddress: c.Total - c.WithoutAddress, WithoutAddress: c.WithoutAddress,
			OnMap: c.OnMap, Approximate: c.Approximate, Pending: c.Pending, NotFound: c.NotFound,
			QuotaExceeded: c.QuotaExceeded, Refused: c.Refused,
			AddressShare: c.AddressShare(), MapShare: c.MapShare(),
		},
	}
	for _, m := range item.Months {
		out.Months = append(out.Months, GeocodingPlatformMonthResponse{CycleStart: utcText(m.CycleStart), Requests: m.Requests})
	}
	return out
}
