package lead

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/geo"
	"vozko/domain/georef"
	geocoding_usecase "vozko/usecases/geocoding"
)

const placeCacheControl = "private, max-age=3600"

type PlaceSuggester interface {
	Suggest(ctx context.Context, kind, text, state, cityCode string) (geocoding_usecase.PlaceAnswer, error)
}

func (h *ReferencePointHandler) WithPlaces(places PlaceSuggester) *ReferencePointHandler {
	h.places = places
	return h
}

type PlaceBoundsResponse struct {
	South float64 `json:"south" example:"-8.155"`
	West  float64 `json:"west" example:"-35.018"`
	North float64 `json:"north" example:"-7.929"`
	East  float64 `json:"east" example:"-34.858"`
}

type PlaceSuggestionResponse struct {
	Kind         string               `json:"kind" example:"street" enums:"city,district,street,cep"`
	Label        string               `json:"label" example:"Rua Boa Hora, Pina, Recife, PE"`
	Name         string               `json:"name" example:"Rua Boa Hora"`
	ZipCode      string               `json:"zipCode,omitempty" example:"51030300"`
	Street       string               `json:"street,omitempty" example:"Rua Boa Hora"`
	District     string               `json:"district,omitempty" example:"Pina"`
	City         string               `json:"city" example:"Recife"`
	CityCode     string               `json:"cityCode" example:"2611606"`
	CityKey      string               `json:"cityKey" example:"pe:recife"`
	DistrictPair string               `json:"districtPair,omitempty" example:"pe:recife/pina"`
	State        string               `json:"state" example:"PE"`
	Lat          float64              `json:"lat" example:"-8.09"`
	Lng          float64              `json:"lng" example:"-34.88"`
	Precision    string               `json:"precision" example:"street" enums:"street,postal_code,district,city"`
	Bounds       *PlaceBoundsResponse `json:"bounds,omitempty"`
	AddressCount int64                `json:"addressCount" example:"120"`
	ZipCount     int64                `json:"zipCount,omitempty" example:"1"`
}

type PlaceSuggestionsResponse struct {
	Items         []PlaceSuggestionResponse `json:"items"`
	CoveredStates []string                  `json:"coveredStates" example:"DF,PE"`
	Attribution   string                    `json:"attribution" example:"IBGE, CNEFE 2022"`
}

var placeSearchStatus = map[string]int{
	"place_query_invalid":   http.StatusBadRequest,
	"place_scope_invalid":   http.StatusBadRequest,
	"reference_not_loaded":  http.StatusUnprocessableEntity,
	"reference_unavailable": http.StatusServiceUnavailable,
}

var placeSearchMessages = map[string]string{
	"place_query_invalid":   "Digite de 2 a 60 caracteres, ou de 5 a 8 dígitos de um CEP",
	"place_scope_invalid":   "Escolha uma UF válida e, para bairro ou rua, a cidade",
	"reference_not_loaded":  "Este estado ainda não tem base de endereços carregada",
	"reference_unavailable": "A base de endereços não pôde ser lida agora",
}

// @Summary		Buscar lugares para o mapa
// @Description	Busca cidades, bairros, ruas e CEPs na base de referência do IBGE (CNEFE 2022) carregada no servidor, sem chamar provedor externo e sem gravar nada. `q` com 5 a 8 dígitos (com ou sem hífen) busca CEPs pelo prefixo; outro texto (2 a 60 caracteres) busca pelo início do nome, sem diferenciar acento nem maiúsculas, em cidades, bairros e ruas ao mesmo tempo. Para ruas, uma palavra de tipo no início (rua, av, travessa) é ignorada. Até 10 resultados: primeiro os nomes iguais ao texto, depois os com mais endereços, agrupados por tipo (cidade, bairro, rua, CEP). Cada item traz o rótulo, a UF, a cidade com o código IBGE e a chave do filtro de cidade (`cityKey`), o bairro com a chave do filtro de bairro (`districtPair`) quando houver, o ponto de referência com a precisão e, para cidade e bairro, a caixa (`bounds`) para enquadrar o mapa. Uma rua com mais de um CEP no mesmo bairro traz `zipCount` e nenhum `zipCode`. `coveredStates` lista as UFs carregadas. `state` e `cityCode` restringem a busca. As respostas ficam em cache até a próxima carga da base (`Cache-Control: private, max-age=3600`). Recusas: 400 `place_query_invalid`, 400 `place_scope_invalid`, 422 `reference_not_loaded` (a UF pedida, ou nenhuma, não está carregada; `expected.coveredStates` traz as UFs carregadas separadas por vírgula), 503 `reference_unavailable`, 503 `place_search_unavailable` (rota não ligada) e 503 com `Retry-After` quando o servidor está ocupado.
// @Tags			Leads
// @Produce		json
// @Param			q			query		string	true	"Texto ou CEP (2 a 60 caracteres)"
// @Param			state		query		string	false	"UF ou nome do estado"
// @Param			cityCode	query		string	false	"Código IBGE da cidade (7 dígitos)"
// @Success		200			{object}	lead.PlaceSuggestionsResponse
// @Failure		400			{object}	response.ErrorResponse	"place_query_invalid, place_scope_invalid"
// @Failure		403			{object}	response.ErrorResponse	"forbidden"
// @Failure		422			{object}	response.ErrorResponse	"reference_not_loaded"
// @Failure		503			{object}	response.ErrorResponse	"reference_unavailable, place_search_unavailable, busy"
// @Security		BearerAuth
// @Router			/leads/places/search [get]
func (h *ReferencePointHandler) SearchPlaces(w http.ResponseWriter, r *http.Request) {
	h.answerPlaces(w, r, "")
}

// @Summary		Sugerir cidade, bairro, rua ou CEP de um endereço
// @Description	Autocompletar dos campos de endereço do lead, pela mesma base, regras e cache de `GET /leads/places/search`, restrito a um tipo. `kind=city` busca cidades (opcionalmente da `state`); `kind=district` e `kind=street` precisam de `cityCode` e buscam bairros ou ruas dessa cidade; `kind=cep` busca CEPs por 5 a 8 dígitos. Uma rua escolhida traz CEP (quando a rua tem um só CEP no bairro), bairro, cidade, UF e o ponto de referência, para preencher o endereço e posicionar o alfinete. Recusas iguais às da busca do mapa; sem `kind` responde 400 `place_query_invalid`.
// @Tags			Leads
// @Produce		json
// @Param			kind		query		string	true	"Tipo"	Enums(city, district, street, cep)
// @Param			q			query		string	true	"Início do nome ou do CEP (2 a 60 caracteres)"
// @Param			state		query		string	false	"UF ou nome do estado"
// @Param			cityCode	query		string	false	"Código IBGE da cidade (obrigatório para bairro e rua)"
// @Success		200			{object}	lead.PlaceSuggestionsResponse
// @Failure		400			{object}	response.ErrorResponse	"place_query_invalid, place_scope_invalid"
// @Failure		403			{object}	response.ErrorResponse	"forbidden"
// @Failure		422			{object}	response.ErrorResponse	"reference_not_loaded"
// @Failure		503			{object}	response.ErrorResponse	"reference_unavailable, place_search_unavailable, busy"
// @Security		BearerAuth
// @Router			/leads/places/suggest [get]
func (h *ReferencePointHandler) SuggestPlaces(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		writePlaceRefusal(w, "place_query_invalid", nil)
		return
	}
	h.answerPlaces(w, r, kind)
}

func (h *ReferencePointHandler) answerPlaces(w http.ResponseWriter, r *http.Request, kind string) {
	w.Header().Set("Cache-Control", "no-store")
	if h.places == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "place_search_unavailable", "A busca de lugares não está disponível neste servidor", nil)
		return
	}
	values := r.URL.Query()
	answer, err := h.places.Suggest(r.Context(), kind, values.Get("q"), values.Get("state"), values.Get("cityCode"))
	if err != nil {
		writePlaceError(w, err, answer.CoveredStates)
		return
	}
	items := make([]PlaceSuggestionResponse, 0, len(answer.Places))
	for _, p := range answer.Places {
		items = append(items, placeResponse(p))
	}
	covered := answer.CoveredStates
	if covered == nil {
		covered = []string{}
	}
	w.Header().Set("Cache-Control", placeCacheControl)
	response.WriteSuccess(w, http.StatusOK, PlaceSuggestionsResponse{Items: items, CoveredStates: covered, Attribution: georef.Attribution})
}

func placeResponse(p georef.Place) PlaceSuggestionResponse {
	out := PlaceSuggestionResponse{
		Kind: string(p.Kind), Label: p.LabelText(), Name: p.Name, ZipCode: p.ZipCode, Street: p.Street, District: p.District,
		City: p.City, CityCode: p.CityCode, CityKey: p.CityKey(), DistrictPair: p.DistrictPair(), State: p.State, Lat: p.Point.Lat, Lng: p.Point.Lng, Precision: string(p.Precision),
		AddressCount: p.AddressCount, ZipCount: p.ZipCount,
	}
	if p.Bounds != nil {
		out.Bounds = &PlaceBoundsResponse{South: p.Bounds.South, West: p.Bounds.West, North: p.Bounds.North, East: p.Bounds.East}
	}
	return out
}

func placeErrorCode(err error) string {
	switch {
	case errors.Is(err, georef.ErrPlaceQueryInvalid):
		return "place_query_invalid"
	case errors.Is(err, georef.ErrPlaceScopeInvalid):
		return "place_scope_invalid"
	}
	return geo.ReferenceErrorCode(err)
}

func writePlaceError(w http.ResponseWriter, err error, covered []string) {
	if httpx.WriteAnalyticsLimit(w, err, "place search") {
		return
	}
	code := placeErrorCode(err)
	status, known := placeSearchStatus[code]
	if !known || status >= http.StatusInternalServerError {
		log.Printf("[lead-places] place search failed: %v", err)
	}
	if !known {
		response.WriteError(w, http.StatusInternalServerError, "Falha ao buscar lugares", nil)
		return
	}
	if code != "reference_not_loaded" {
		covered = nil
	}
	writePlaceRefusal(w, code, covered)
}

func writePlaceRefusal(w http.ResponseWriter, code string, covered []string) {
	var expected map[string]string
	if covered != nil {
		expected = map[string]string{"coveredStates": strings.Join(covered, ",")}
	}
	response.WriteErrorWithCode(w, placeSearchStatus[code], code, placeSearchMessages[code], expected)
}
