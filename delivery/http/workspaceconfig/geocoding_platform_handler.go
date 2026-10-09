package workspaceconfig

import (
	"context"
	"errors"
	"net/http"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/geocoding"
)

type GeocodingPlatformService interface {
	List(ctx context.Context, editor geocoding.Editor, query geocoding.PlatformQuery) (geocoding.PlatformPage, error)
}

func (h *WorkspaceConfigHandler) SetGeocodingPlatformUsage(service GeocodingPlatformService) {
	h.geocodingPlatform = service
}

func (h *WorkspaceConfigHandler) ListGeocodingPlatformUsage(w http.ResponseWriter, r *http.Request) {
	caller, ok := sessionCaller(w, r)
	if !ok {
		return
	}
	if h.geocodingPlatform == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "geocoding_unavailable", "A geolocalização de endereços não está disponível neste servidor", nil)
		return
	}
	values := r.URL.Query()
	pagination := httpx.ParsePagination(values)
	query := geocoding.PlatformQuery{Page: pagination.Page, PageSize: pagination.PageSize, Search: values.Get("search")}
	page, err := h.geocodingPlatform.List(r.Context(), geocoding.Editor{UserID: caller.UserID, PlatformAdmin: caller.PlatformAdmin}, query)
	if err != nil {
		writeGeocodingPlatformError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toGeocodingPlatformResponse(page))
}

func writeGeocodingPlatformError(w http.ResponseWriter, err error) {
	if errors.Is(err, geocoding.ErrPlatformForbidden) {
		response.WriteErrorWithCode(w, http.StatusForbidden, "geocoding_platform_forbidden", "Só um administrador da plataforma pode ver a geolocalização de todos os workspaces", nil)
		return
	}
	if httpx.WriteAnalyticsLimit(w, err, "Geocoding") {
		return
	}
	response.WriteErrorWithCode(w, http.StatusInternalServerError, "geocoding_usage_unreadable", "O consumo de geolocalização não pôde ser lido", nil)
}
