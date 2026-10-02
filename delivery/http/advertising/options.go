package advertisinghttp

import (
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
)

type RouteResponse struct {
	Destination advertising.Destination        `json:"destination"`
	Goals       []advertising.OptimizationGoal `json:"goals"`
}

type ObjectiveOptionResponse struct {
	Objective advertising.Objective `json:"objective"`
	Routes    []RouteResponse       `json:"routes"`
}

type OptionsResponse struct {
	Objectives         []ObjectiveOptionResponse                              `json:"objectives"`
	CallsToAction      []advertising.CallToAction                             `json:"callsToAction"`
	DestinationCTAs    map[advertising.Destination][]advertising.CallToAction `json:"destinationCallsToAction"`
	Placements         map[string][]string                                    `json:"placements"`
	BreakdownGroups    [][]advertising.Breakdown                              `json:"breakdownGroups"`
	AttributionWindows []advertising.AttributionWindow                        `json:"attributionWindows"`
	MatchKeys          []advertising.MatchKey                                 `json:"matchKeys"`
	RuleMetrics        []advertising.RuleMetric                               `json:"ruleMetrics"`
	PixelEvents        []advertising.PixelEvent                               `json:"pixelEvents"`
	Formats            []advertising.CreativeFormat                           `json:"formats"`
}

func buildOptions() OptionsResponse {
	return OptionsResponse{
		Objectives: presentAll(advertising.AllObjectives(), func(o advertising.Objective) ObjectiveOptionResponse {
			return ObjectiveOptionResponse{Objective: o, Routes: presentAll(o.Routes(), func(route advertising.Route) RouteResponse {
				return RouteResponse{Destination: route.Destination, Goals: append([]advertising.OptimizationGoal(nil), route.Goals...)}
			})}
		}),
		CallsToAction:      advertising.LinkCallsToAction(),
		DestinationCTAs:    destinationCallsToAction(),
		Placements:         advertising.PlatformPositions(),
		BreakdownGroups:    advertising.BreakdownGroups(),
		AttributionWindows: advertising.AttributionWindows(),
		MatchKeys:          advertising.AllMatchKeys(),
		RuleMetrics:        advertising.RuleMetrics(),
		PixelEvents:        advertising.PixelEvents(),
		Formats:            advertising.CreativeFormats(),
	}
}

// @Summary		Opções para criar anúncios
// @Description	Listas que a Meta aceita: objetivos com destinos e metas de otimização permitidos, chamadas para ação, posicionamentos por plataforma, combinações de quebras, janelas de atribuição, chaves de correspondência, métricas de regras, eventos de pixel e formatos de criativo.
// @Tags			Anúncios
// @Produce		json
// @Success		200	{object}	OptionsResponse
// @Security		BearerAuth
// @Router			/ads/options [get]
func (h *Handler) Options(w http.ResponseWriter, _ *http.Request) {
	response.WriteSuccess(w, http.StatusOK, buildOptions())
}

func destinationCallsToAction() map[advertising.Destination][]advertising.CallToAction {
	out := map[advertising.Destination][]advertising.CallToAction{}
	for _, o := range advertising.AllObjectives() {
		for _, route := range o.Routes() {
			out[route.Destination] = advertising.CallsToActionFor(route.Destination)
		}
	}
	return out
}
