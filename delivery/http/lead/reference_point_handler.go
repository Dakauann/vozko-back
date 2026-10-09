package lead

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/georef"
	workspace_domain "vozko/domain/workspace"
)

type ReferencePointLocator interface {
	Locate(ctx context.Context, raw address.Postal) (geo.ReferenceSpot, error)
}

type ReferencePointHandler struct {
	points ReferencePointLocator
	places PlaceSuggester
}

func NewReferencePointHandler(points ReferencePointLocator) *ReferencePointHandler {
	return &ReferencePointHandler{points: points}
}

func RegisterReferencePointRoutes(
	protected *mux.Router,
	h *ReferencePointHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		h = NewReferencePointHandler(nil)
	}
	protected.HandleFunc("/leads/map/reference-point", ac(workspace_domain.ResourceLeads, workspace_domain.ActionReadAddresses, h.ReferencePoint)).Methods(http.MethodGet)
	protected.HandleFunc("/leads/places/search", ac(workspace_domain.ResourceLeads, workspace_domain.ActionReadAddresses, h.SearchPlaces)).Methods(http.MethodGet)
	protected.HandleFunc("/leads/places/suggest", ac(workspace_domain.ResourceLeads, workspace_domain.ActionReadAddresses, h.SuggestPlaces)).Methods(http.MethodGet)
}

type ReferencePointResponse struct {
	Lat         float64 `json:"lat" example:"-23.5614"`
	Lng         float64 `json:"lng" example:"-46.6559"`
	Precision   string  `json:"precision" example:"street" enums:"street,postal_code,district,city"`
	City        string  `json:"city" example:"São Carlos"`
	State       string  `json:"state" example:"SP"`
	WholeCity   bool    `json:"wholeCity" example:"true"`
	Attribution string  `json:"attribution" example:"IBGE, CNEFE 2022"`
}

var referencePointStatus = map[string]int{
	"reference_query_invalid":   http.StatusBadRequest,
	"reference_not_loaded":      http.StatusUnprocessableEntity,
	"reference_point_not_found": http.StatusNotFound,
	"reference_unavailable":     http.StatusServiceUnavailable,
}

// @Summary		Ponto de referência de um CEP ou endereço
// @Description	Acha o ponto de referência do IBGE (CNEFE 2022) para um CEP ou um endereço, sem gravar nada e sem chamar provedor externo. Escolhe o melhor ponto entre o do CEP (`street` com dispersão abaixo de 300 m, `postal_code` até 1.500 m, `city` acima disso ou para CEP genérico terminado em 000), o do bairro (`district`, pelo par cidade e bairro) e o da cidade (`city`). Quando o melhor ponto só identifica a cidade (CEP genérico, CEP espalhado pela cidade ou só cidade com UF), `wholeCity` vem `true` e o ponto é o centro da cidade da base, para o raio partir dele. `city` e `state` nomeiam a cidade do ponto quando a base a conhece (vazios caso contrário). Precisa de um `zipCode` válido ou de `city` com `state`; `district` refina quando não há CEP. Serve de centro para o raio a partir de um endereço ou CEP no mapa e de início do pino no editor de endereço. `attribution` é a fonte a citar. Recusas: 400 `reference_query_invalid` (sem CEP válido nem cidade com UF, ou campo inválido), 422 `reference_not_loaded` (a base de referência não está carregada para a UF do lugar; com só parte das UFs carregadas, um CEP fora da base e sem UF também cai aqui), 404 `reference_point_not_found` (a UF está carregada, mas não há ponto para o lugar), 503 `reference_unavailable` (a base não pôde ser lida) e 503 `reference_point_unavailable` (a rota não está ligada).
// @Tags			Leads
// @Produce		json
// @Param			zipCode		query		string	false	"CEP com ou sem máscara"
// @Param			district	query		string	false	"Bairro"
// @Param			city		query		string	false	"Cidade"
// @Param			state		query		string	false	"UF ou nome do estado"
// @Success		200			{object}	lead.ReferencePointResponse
// @Failure		400			{object}	response.ErrorResponse	"reference_query_invalid"
// @Failure		403			{object}	response.ErrorResponse	"forbidden"
// @Failure		404			{object}	response.ErrorResponse	"reference_point_not_found"
// @Failure		422			{object}	response.ErrorResponse	"reference_not_loaded"
// @Failure		503			{object}	response.ErrorResponse	"reference_unavailable, reference_point_unavailable"
// @Security		BearerAuth
// @Router			/leads/map/reference-point [get]
func (h *ReferencePointHandler) ReferencePoint(w http.ResponseWriter, r *http.Request) {
	if h.points == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "reference_point_unavailable", "O ponto de referência não está disponível neste servidor", nil)
		return
	}
	values := r.URL.Query()
	spot, err := h.points.Locate(r.Context(), address.Postal{
		ZipCode:  values.Get("zipCode"),
		District: values.Get("district"),
		City:     values.Get("city"),
		State:    values.Get("state"),
	})
	if err != nil {
		writeReferencePointError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ReferencePointResponse{
		Lat: spot.Fix.Point.Lat, Lng: spot.Fix.Point.Lng, Precision: string(spot.Fix.Precision),
		City: spot.City, State: spot.State, WholeCity: spot.WholeCity, Attribution: georef.Attribution,
	})
}

func writeReferencePointError(w http.ResponseWriter, err error) {
	code := geo.ReferenceErrorCode(err)
	status, known := referencePointStatus[code]
	if !known || status >= http.StatusInternalServerError {
		log.Printf("[lead-map] reference point failed: %v", err)
	}
	if !known {
		response.WriteError(w, http.StatusInternalServerError, "Falha ao achar o ponto de referência", nil)
		return
	}
	message := "O ponto de referência não pôde ser achado"
	if errors.Is(err, geo.ErrReferenceQueryInvalid) {
		message = "Informe um CEP válido ou a cidade com a UF"
	}
	response.WriteErrorWithCode(w, status, code, message, nil)
}
