package workspaceconfig

import (
	"vozko/domain/geocoding"
)

type GeocodingPlatformResponse struct {
	Items      []GeocodingPlatformWorkspaceResponse `json:"items"`
	Page       int                                  `json:"page"`
	PageSize   int                                  `json:"pageSize"`
	TotalItems int64                                `json:"totalItems"`
	TotalPages int                                  `json:"totalPages"`
}

type GeocodingPlatformWorkspaceResponse struct {
	WorkspaceID   string                            `json:"workspaceId"`
	WorkspaceName string                            `json:"workspaceName"`
	Coverage      GeocodingPlatformCoverageResponse `json:"coverage"`
}

type GeocodingPlatformCoverageResponse struct {
	Total          int64   `json:"total"`
	WithAddress    int64   `json:"withAddress"`
	WithoutAddress int64   `json:"withoutAddress"`
	OnMap          int64   `json:"onMap"`
	Approximate    int64   `json:"approximate"`
	Pending        int64   `json:"pending"`
	NotFound       int64   `json:"notFound"`
	AddressShare   float64 `json:"addressShare"`
	MapShare       float64 `json:"mapShare"`
}

func toGeocodingPlatformResponse(page geocoding.PlatformPage) GeocodingPlatformResponse {
	out := GeocodingPlatformResponse{
		Items:      make([]GeocodingPlatformWorkspaceResponse, 0, len(page.Items)),
		Page:       page.Page,
		PageSize:   page.PageSize,
		TotalItems: page.TotalItems,
		TotalPages: page.TotalPages(),
	}
	for _, item := range page.Items {
		out.Items = append(out.Items, toGeocodingPlatformWorkspace(item))
	}
	return out
}

func toGeocodingPlatformWorkspace(item geocoding.WorkspaceCoverage) GeocodingPlatformWorkspaceResponse {
	c := item.Coverage
	return GeocodingPlatformWorkspaceResponse{
		WorkspaceID:   item.Workspace.ID,
		WorkspaceName: item.Workspace.Name,
		Coverage: GeocodingPlatformCoverageResponse{
			Total: c.Total, WithAddress: c.Total - c.WithoutAddress, WithoutAddress: c.WithoutAddress,
			OnMap: c.OnMap, Approximate: c.Approximate, Pending: c.Pending, NotFound: c.NotFound,
			AddressShare: c.AddressShare(), MapShare: c.MapShare(),
		},
	}
}
