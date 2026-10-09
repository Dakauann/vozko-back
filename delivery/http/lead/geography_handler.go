package lead

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	leaddomain "vozko/domain/lead"
	"vozko/domain/leadarea"
	"vozko/domain/leadmap"
	workspace_domain "vozko/domain/workspace"
	lead_usecase "vozko/usecases/lead"
)

type MapSections interface {
	Summary(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter) (leadmap.Summary, error)
	Layer(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter, q lead_usecase.LayerQuery) (leadmap.Layer, error)
	Tile(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter, q lead_usecase.TileQuery) (leadmap.Layer, error)
	Districts(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter) ([]leadmap.District, error)
	Viewport(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter) (leadmap.Viewport, error)
	PointLeads(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter, at geo.Point, placement crmfilter.GeoPlacement) (lead_usecase.MapPeek, error)
	LeftOut(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter) (leadmap.LeftOut, error)
}

type DrawnAreas interface {
	Create(ctx context.Context, a lead_usecase.Actor, d leadarea.Draft) (leadarea.Area, error)
	List(ctx context.Context, a lead_usecase.Actor) ([]leadarea.Area, error)
	Get(ctx context.Context, a lead_usecase.Actor, id string) (leadarea.Area, error)
	Update(ctx context.Context, a lead_usecase.Actor, id string, p leadarea.Patch) (leadarea.Area, error)
	Delete(ctx context.Context, a lead_usecase.Actor, id string) error
}

const (
	areaIDPath  = leadIDPath
	maxAreaBody = 64 << 10
)

type GeographyHandler struct {
	maps  MapSections
	areas DrawnAreas
	now   func() time.Time
}

func NewGeographyHandler(maps MapSections, areas DrawnAreas, now func() time.Time) *GeographyHandler {
	if now == nil {
		now = time.Now
	}
	return &GeographyHandler{maps: maps, areas: areas, now: now}
}

func RegisterGeographyRoutes(
	protected *mux.Router,
	h *GeographyHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	if h == nil {
		h = NewGeographyHandler(nil, nil, nil)
	}
	ld, addresses := workspace_domain.ResourceLeads, workspace_domain.ActionReadAddresses
	protected.HandleFunc("/leads/map/summary", ac(ld, addresses, h.MapSummary)).Methods(http.MethodGet)
	protected.HandleFunc("/leads/map/layer", ac(ld, addresses, h.MapLayer)).Methods(http.MethodGet)
	protected.HandleFunc("/leads/map/tiles/{z}/{x}/{y}", ac(ld, addresses, h.MapTile)).Methods(http.MethodGet)
	protected.HandleFunc("/leads/map/districts", ac(ld, addresses, h.MapDistricts)).Methods(http.MethodGet)
	protected.HandleFunc("/leads/map/viewport", ac(ld, addresses, h.MapViewport)).Methods(http.MethodGet)
	protected.HandleFunc("/leads/map/point", ac(ld, addresses, h.MapPoint)).Methods(http.MethodGet)
	protected.HandleFunc("/leads/map/left-out", ac(ld, addresses, h.MapLeftOut)).Methods(http.MethodGet)
	protected.HandleFunc("/lead-areas", ac(ld, addresses, h.ListAreas)).Methods(http.MethodGet)
	protected.HandleFunc("/lead-areas", ac(ld, addresses, h.CreateArea)).Methods(http.MethodPost)
	protected.HandleFunc("/lead-areas"+areaIDPath, ac(ld, addresses, h.GetArea)).Methods(http.MethodGet)
	protected.HandleFunc("/lead-areas"+areaIDPath, ac(ld, addresses, h.UpdateArea)).Methods(http.MethodPatch)
	protected.HandleFunc("/lead-areas"+areaIDPath, ac(ld, addresses, h.DeleteArea)).Methods(http.MethodDelete)
}

func (h *GeographyHandler) mapRequest(w http.ResponseWriter, r *http.Request) (lead_usecase.Actor, crmfilter.Filter, bool) {
	if h.maps == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "lead_map_unavailable", "O mapa de leads não está disponível neste servidor", nil)
		return lead_usecase.Actor{}, crmfilter.Filter{}, false
	}
	a, ok := actorOf(r)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return lead_usecase.Actor{}, crmfilter.Filter{}, false
	}
	input, err := listInputFromQuery(a.WorkspaceID, r.URL.Query())
	if err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, leaddomain.ErrorCode(leaddomain.ErrLeadFilterInvalid), "O filtro enviado não é válido", nil)
		return lead_usecase.Actor{}, crmfilter.Filter{}, false
	}
	return a, input.Filter, true
}

func writeMapError(w http.ResponseWriter, err error) {
	if httpx.WriteAnalyticsLimit(w, err, "Lead map") || writeFilterRefusal(w, err) {
		return
	}
	switch {
	case errors.Is(err, leaddomain.ErrLeadForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, leaddomain.ErrorCode(err), err.Error(), nil)
	case errors.Is(err, leadmap.ErrColorByForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, leadmap.ErrorCode(err), err.Error(), nil)
	case leadmap.ErrorCode(err) != "":
		response.WriteErrorWithCode(w, http.StatusBadRequest, leadmap.ErrorCode(err), err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Falha ao montar o mapa de leads", nil)
	}
}

func writeWindowRefusal(w http.ResponseWriter, message string) {
	response.WriteErrorWithCode(w, http.StatusBadRequest, "map_window_invalid", message, nil)
}

func parseCoordinates(raw string, n int) ([]float64, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) != n {
		return nil, false
	}
	out := make([]float64, n)
	for i, part := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// @Summary		Resumo do mapa de leads
// @Description	Conta, em uma única passada, os leads do filtro por situação de posição no endereço principal: `total`, `onMap` (posição que identifica a casa: exata, endereço ou rua), `approximate` (posição de CEP, bairro ou cidade), `withoutAddress`, `notFound` (não localizado ou ambíguo), `pending` (aguardando geocodificação), `quotaExceeded` e `refused` (texto recusado pelo provedor, sem posição). As categorias somam o total. Cada categoria é o filtro `geo_placement` de mesmo nome (`on_map`, `approximate`, `without_address`, `not_found`, `pending`, `quota_exceeded`, `refused`), com a mesma conta, para virar link em GET /leads. Aceita os mesmos filtros de GET /leads (inclusive `area`, resolvida antes de compilar o filtro). Resultado em cache por 60 s por workspace, geração dos leads e filtro; responde 503 com `Retry-After` quando o servidor está ocupado.
// @Tags			Leads
// @Produce		json
// @Param			filter	query		string	false	"Filtro crmfilter em JSON (opcionalmente em base64)"
// @Param			q		query		string	false	"Busca livre"
// @Success		200		{object}	lead.MapSummaryResponse
// @Failure		400		{object}	response.ErrorResponse	"lead_filter_invalid, custom_field_filter_*, area_not_found, area_too_many, area_operator_unsupported"
// @Failure		403		{object}	response.ErrorResponse	"forbidden, custom_field_filter_sensitive_forbidden, lead_filter_address_forbidden, lead_addresses_forbidden"
// @Failure		503		{object}	response.ErrorResponse	"lead_map_unavailable, ou consultas analíticas ocupadas (com Retry-After)"
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/map/summary [get]
func (h *GeographyHandler) MapSummary(w http.ResponseWriter, r *http.Request) {
	a, f, ok := h.mapRequest(w, r)
	if !ok {
		return
	}
	summary, err := h.maps.Summary(r.Context(), a, f)
	if err != nil {
		writeMapError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, mapSummaryResponse(summary))
}

// @Summary		Camada do mapa de leads
// @Description	Devolve a camada da janela do mapa. A janela é ajustada aos tiles XYZ (zoom inteiro, no máximo 32 tiles por eixo). Até 5.000 posições distintas a resposta é `{"kind":"points","points":[...]}`: um ponto por posição e classe (`placement`), com `count` pessoas, até 5 `leadIds`, a melhor `precision` e o `tone` do valor mais comum do campo `colorBy` (`neutral` sem campo ou sem valor). Acima disso a resposta é `{"kind":"cells","cellSizeDegrees":s,"cells":[...]}`, com `ix = floor(lng / s)`, `iy = floor(lat / s)` e uma célula por classe. Entram os endereços principais com posição: `placement` `on_map` quando a posição identifica a casa (a contagem No mapa), `approximate` quando é o ponto de referência do CEP, bairro ou cidade (a contagem Aproximados). As duas classes nunca se juntam no mesmo ponto ou célula; uma área desenhada continua reunindo só `on_map`. `colorBy` precisa ser um campo de seleção de lead; um campo sensível exige `leads:read_sensitive`.
// @Tags			Leads
// @Produce		json
// @Param			bbox	query		string	true	"Janela como oeste,sul,leste,norte (lng,lat,lng,lat)"
// @Param			zoom	query		number	true	"Zoom do mapa (0 a 22)"
// @Param			colorBy	query		string	false	"Chave do campo de seleção que colore os pontos"
// @Param			filter	query		string	false	"Filtro crmfilter em JSON (opcionalmente em base64)"
// @Success		200		{object}	lead.MapLayerResponse
// @Failure		400		{object}	response.ErrorResponse	"map_window_invalid, map_window_too_wide, map_color_by_invalid, lead_filter_invalid, custom_field_filter_*, area_not_found, area_too_many, area_operator_unsupported"
// @Failure		403		{object}	response.ErrorResponse	"forbidden, custom_field_filter_sensitive_forbidden, lead_filter_address_forbidden, lead_addresses_forbidden, map_color_by_forbidden"
// @Failure		503		{object}	response.ErrorResponse	"lead_map_unavailable, ou consultas analíticas ocupadas (com Retry-After)"
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/map/layer [get]
func (h *GeographyHandler) MapLayer(w http.ResponseWriter, r *http.Request) {
	a, f, ok := h.mapRequest(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	box, ok := parseCoordinates(values.Get("bbox"), 4)
	if !ok {
		writeWindowRefusal(w, "bbox deve ser oeste,sul,leste,norte")
		return
	}
	zoom, err := strconv.ParseFloat(strings.TrimSpace(values.Get("zoom")), 64)
	if err != nil {
		writeWindowRefusal(w, "zoom é obrigatório")
		return
	}
	layer, err := h.maps.Layer(r.Context(), a, f, lead_usecase.LayerQuery{
		BBox:    geo.BBox{West: box[0], South: box[1], East: box[2], North: box[3]},
		Zoom:    zoom,
		ColorBy: values.Get("colorBy"),
	})
	if err != nil {
		writeMapError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, mapLayerResponse(layer))
}

// @Summary		Tile do mapa de leads
// @Description	Devolve um tile XYZ do mapa (`z` de 0 a 22, `x` e `y` dentro da grade do zoom). O tile reúne os endereços principais com posição que ele cobre: longitude de oeste (incluso) a leste (excluso) e latitude de sul (excluso) a norte (incluso), de modo que um ponto na borda pertence a um único tile e a soma dos tiles nunca conta alguém duas vezes. Até 1.000 posições distintas a resposta é `{"kind":"points","points":[...]}`: um ponto por posição e classe (`placement`), com `count` pessoas, até 5 `leadIds`, a melhor `precision` e o `tone` do valor mais comum do campo `colorBy` (`neutral` sem campo ou sem valor). Acima disso a resposta é `{"kind":"cells","cellSizeDegrees":s,"cells":[...]}`, com `s` igual a 1/32 da largura do tile, `ix = floor(lng / s)`, `iy = floor(lat / s)` e uma célula por classe; uma célula na borda de latitude de dois tiles chega em parte por cada um, e o cliente soma as partes de mesmo `ix`, `iy` e `placement`. `placement` é `on_map` quando a posição identifica a casa (a contagem No mapa) e `approximate` quando é o ponto de referência do CEP, bairro ou cidade (a contagem Aproximados); as duas classes nunca se juntam. Aceita os mesmos filtros de GET /leads (inclusive `area`, que só reúne `on_map`). `colorBy` precisa ser um campo de seleção de lead; um campo sensível exige `leads:read_sensitive`. Cada tile fica em cache por 60 s por workspace, geração dos leads, nível de campos de quem lê, filtro, hora da última alteração de cada área, tile e `colorBy`; responde 503 com `Retry-After` quando o servidor está ocupado.
// @Tags			Leads
// @Produce		json
// @Param			z		path		int		true	"Zoom do tile (0 a 22)"
// @Param			x		path		int		true	"Coluna do tile (0 a 2^z - 1)"
// @Param			y		path		int		true	"Linha do tile (0 a 2^z - 1)"
// @Param			colorBy	query		string	false	"Chave do campo de seleção que colore os pontos"
// @Param			filter	query		string	false	"Filtro crmfilter em JSON (opcionalmente em base64)"
// @Param			q		query		string	false	"Busca livre"
// @Success		200		{object}	lead.MapLayerResponse
// @Failure		400		{object}	response.ErrorResponse	"map_tile_invalid, map_color_by_invalid, lead_filter_invalid, custom_field_filter_*, area_not_found, area_too_many, area_operator_unsupported"
// @Failure		403		{object}	response.ErrorResponse	"forbidden, custom_field_filter_sensitive_forbidden, lead_filter_address_forbidden, lead_addresses_forbidden, map_color_by_forbidden"
// @Failure		503		{object}	response.ErrorResponse	"lead_map_unavailable, ou consultas analíticas ocupadas (com Retry-After)"
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/map/tiles/{z}/{x}/{y} [get]
func (h *GeographyHandler) MapTile(w http.ResponseWriter, r *http.Request) {
	a, f, ok := h.mapRequest(w, r)
	if !ok {
		return
	}
	q, ok := tileQueryOf(mux.Vars(r), r.URL.Query().Get("colorBy"))
	if !ok {
		response.WriteErrorWithCode(w, http.StatusBadRequest, leadmap.ErrorCode(geo.ErrInvalidTile), "z, x e y devem ser números inteiros", nil)
		return
	}
	layer, err := h.maps.Tile(r.Context(), a, f, q)
	if err != nil {
		writeMapError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, mapLayerResponse(layer))
}

func tileQueryOf(vars map[string]string, colorBy string) (lead_usecase.TileQuery, bool) {
	var xyz [3]int
	for i, name := range []string{"z", "x", "y"} {
		v, err := strconv.Atoi(vars[name])
		if err != nil {
			return lead_usecase.TileQuery{}, false
		}
		xyz[i] = v
	}
	return lead_usecase.TileQuery{Z: xyz[0], X: xyz[1], Y: xyz[2], ColorBy: colorBy}, true
}

// @Summary		Leads por bairro
// @Description	Conta os leads do filtro por par cidade e bairro do endereço principal, incluindo os aproximados e os ainda sem posição, com o nome mais comum do bairro, `pair` (o valor do filtro `district`) e o ponto do bairro: o ponto de referência do IBGE (CNEFE 2022) quando o bairro está carregado, achado pelo código IBGE do endereço ou pela cidade e UF; senão a média das posições não mais grosseiras que o bairro. Um bairro sem nenhum lead localizado aparece quando tem ponto de referência; os que não têm ponto nenhum ficam de fora. No máximo 2.000, do maior para o menor. Em cache por 60 s.
// @Tags			Leads
// @Produce		json
// @Param			filter	query	string	false	"Filtro crmfilter em JSON (opcionalmente em base64)"
// @Success		200		{array}	lead.MapDistrictResponse
// @Failure		400		{object}	response.ErrorResponse	"lead_filter_invalid, custom_field_filter_*, area_not_found, area_too_many, area_operator_unsupported"
// @Failure		403		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse	"lead_map_unavailable, ou consultas analíticas ocupadas (com Retry-After)"
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/map/districts [get]
func (h *GeographyHandler) MapDistricts(w http.ResponseWriter, r *http.Request) {
	a, f, ok := h.mapRequest(w, r)
	if !ok {
		return
	}
	districts, err := h.maps.Districts(r.Context(), a, f)
	if err != nil {
		writeMapError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, mapDistrictResponses(districts))
}

// @Summary		Enquadramento inicial do mapa
// @Description	Decide onde o mapa abre para o filtro: com uma área desenhada no filtro (`area` ou `area_approximate`), a caixa que junta todas essas áreas (`basis: area`), mesmo quando nenhum lead cai nelas; senão a caixa dos leads com posição (`basis: located`); senão a cidade mais comum entre os endereços, centrada no ponto de referência da cidade (IBGE), quando ele está carregado (`city`); senão o Brasil (`country`, ainda trazendo a cidade mais comum em `city`). `view` é `districts` (Por bairro) enquanto menos de 20% dos leads do filtro têm posição que identifica a casa, e `positions` no resto. Em cache por 60 s.
// @Tags			Leads
// @Produce		json
// @Param			filter	query		string	false	"Filtro crmfilter em JSON (opcionalmente em base64)"
// @Success		200		{object}	lead.MapViewportResponse
// @Failure		400		{object}	response.ErrorResponse	"lead_filter_invalid, custom_field_filter_*, area_not_found, area_too_many, area_operator_unsupported"
// @Failure		403		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse	"lead_map_unavailable, ou consultas analíticas ocupadas (com Retry-After)"
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/map/viewport [get]
func (h *GeographyHandler) MapViewport(w http.ResponseWriter, r *http.Request) {
	a, f, ok := h.mapRequest(w, r)
	if !ok {
		return
	}
	viewport, err := h.maps.Viewport(r.Context(), a, f)
	if err != nil {
		writeMapError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, mapViewportResponse(viewport))
}

// @Summary		Leads de um ponto do mapa
// @Description	Lista os leads do filtro cujo endereço principal está exatamente na posição de um ponto da camada (use `lat`, `lng` e `placement` do ponto), para a prévia do mapa. `placement` separa a casa localizada (`on_map`, o padrão) da posição aproximada no ponto de referência do CEP, bairro ou cidade (`approximate`), que nunca se misturam no mesmo ponto. Cada item vem projetado como em GET /leads/{id} (campos sensíveis só com `leads:read_sensitive`). No máximo 50 itens, com `total` de pessoas na posição.
// @Tags			Leads
// @Produce		json
// @Param			lat		query		number	true	"Latitude do ponto"
// @Param			lng		query		number	true	"Longitude do ponto"
// @Param			placement	query	string	false	"Classe de posição do ponto (padrão on_map)"	Enums(on_map, approximate)
// @Param			filter	query		string	false	"Filtro crmfilter em JSON (opcionalmente em base64)"
// @Success		200		{object}	lead.MapPeekResponse
// @Failure		400		{object}	response.ErrorResponse	"map_position_invalid, map_placement_invalid, lead_filter_invalid, custom_field_filter_*, area_not_found, area_too_many, area_operator_unsupported"
// @Failure		403		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse	"lead_map_unavailable, ou consultas analíticas ocupadas (com Retry-After)"
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/map/point [get]
func (h *GeographyHandler) MapPoint(w http.ResponseWriter, r *http.Request) {
	a, f, ok := h.mapRequest(w, r)
	if !ok {
		return
	}
	lat, latErr := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("lat")), 64)
	lng, lngErr := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("lng")), 64)
	if latErr != nil || lngErr != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, leadmap.ErrorCode(leadmap.ErrInvalidPosition), "lat e lng são obrigatórios", nil)
		return
	}
	placement, err := leadmap.PointPlacement(r.URL.Query().Get("placement"))
	if err != nil {
		writeMapError(w, err)
		return
	}
	peek, err := h.maps.PointLeads(r.Context(), a, f, geo.Point{Lat: lat, Lng: lng}, placement)
	if err != nil {
		writeMapError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, mapPeekResponse(peek, h.now()))
}

// @Summary		Aproximados que ficaram fora das áreas
// @Description	Uma área desenhada reúne, por padrão, os endereços principais com posição de casa e também os com posição aproximada (CEP, bairro ou cidade) cujo ponto fica dentro dela; nada fica de fora. Só um teste `area` com `"key":"exact_only"` deixa de fora os aproximados. Esta seção conta esses leads: o mesmo filtro, com cada teste `area` `exact_only` lido como `area_approximate` (sem a `key`) e os demais testes de área mantidos. Sem nenhum teste `exact_only` responde `{"total":0,"districts":[]}` sem `filter` e sem consultar o banco. Devolve `total`, os bairros desses leads (no máximo 100, do maior para o menor, com `pair` para o filtro `district`) e `filter`, o filtro completo que a contagem leu (o `filter` estruturado mais os grupos dos parâmetros simples, como `name`, `stageId`, `labelId` ou `createdFrom`) com cada `area` `exact_only` trocado por `area_approximate`, pronto para listar esses leads em GET /leads só com `filter` (some `{"field":"district","operator":"in","values":[pair]}` para listar um bairro). Só a busca livre `q` (ou `search`) não entra em `filter`; continue enviando-a à parte. Uma área `exact_only` num grupo `or` (o padrão de um grupo sem `conjunction`) ao lado de qualquer teste que não seja outra área `exact_only` não tem leitura exata e é recusada com `area_left_out_unsupported`; grupos `and`, ou grupos `or` só de áreas `exact_only`, são aceitos. Sem área no filtro responde `{"total":0,"districts":[]}` sem consultar o banco. Em cache por 60 s por workspace, geração dos leads, filtro e hora da última alteração de cada área.
// @Tags			Leads
// @Produce		json
// @Param			filter	query		string	false	"Filtro crmfilter em JSON (opcionalmente em base64)"
// @Param			q		query		string	false	"Busca livre"
// @Success		200		{object}	lead.MapLeftOutResponse
// @Failure		400		{object}	response.ErrorResponse	"lead_filter_invalid, custom_field_filter_*, area_not_found, area_too_many, area_operator_unsupported, area_left_out_unsupported"
// @Failure		403		{object}	response.ErrorResponse	"forbidden, custom_field_filter_sensitive_forbidden, lead_filter_address_forbidden, lead_addresses_forbidden"
// @Failure		503		{object}	response.ErrorResponse	"lead_map_unavailable, ou consultas analíticas ocupadas (com Retry-After)"
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/map/left-out [get]
func (h *GeographyHandler) MapLeftOut(w http.ResponseWriter, r *http.Request) {
	a, f, ok := h.mapRequest(w, r)
	if !ok {
		return
	}
	link, err := leftOutLink(a.WorkspaceID, r.URL.Query())
	if err != nil {
		writeMapError(w, err)
		return
	}
	left, err := h.maps.LeftOut(r.Context(), a, f)
	if err != nil {
		writeMapError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, mapLeftOutResponse(left, link))
}

func leftOutLink(workspaceID string, values url.Values) (*crmfilter.Filter, error) {
	unsearched := url.Values{}
	for key, v := range values {
		if key != "q" && key != "search" {
			unsearched[key] = v
		}
	}
	input, err := listInputFromQuery(workspaceID, unsearched)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", leaddomain.ErrLeadFilterInvalid, err)
	}
	left, ok, err := leadarea.LeftOut(input.Filter)
	if err != nil || !ok {
		return nil, err
	}
	return &left, nil
}

func (h *GeographyHandler) areaActor(w http.ResponseWriter, r *http.Request) (lead_usecase.Actor, bool) {
	if h.areas == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "lead_areas_unavailable", "As áreas de leads não estão disponíveis neste servidor", nil)
		return lead_usecase.Actor{}, false
	}
	a, ok := actorOf(r)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
	}
	return a, ok
}

func writeAreaError(w http.ResponseWriter, err error) {
	coded := func(status int) {
		response.WriteErrorWithCode(w, status, leadarea.ErrorCode(err), err.Error(), nil)
	}
	switch {
	case errors.Is(err, leadarea.ErrNotFound):
		coded(http.StatusNotFound)
	case errors.Is(err, leadarea.ErrForbidden), errors.Is(err, leadarea.ErrAddressesRequired):
		coded(http.StatusForbidden)
	case leadarea.IsInputRefusal(err):
		coded(http.StatusBadRequest)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Falha ao salvar a área", nil)
	}
}

// @Summary		Listar áreas desenhadas
// @Description	Lista as áreas desenhadas no mapa que a pessoa pode ler: as próprias e as compartilhadas do workspace (no máximo 500). Um workspace guarda no máximo 500 áreas. `canEdit` diz se ela pode renomear, compartilhar ou apagar.
// @Tags			Leads
// @Produce		json
// @Success		200	{object}	lead.DrawnAreaListResponse
// @Failure		403	{object}	response.ErrorResponse	"lead_addresses_forbidden"
// @Security		BearerAuth
// @Router			/lead-areas [get]
func (h *GeographyHandler) ListAreas(w http.ResponseWriter, r *http.Request) {
	a, ok := h.areaActor(w, r)
	if !ok {
		return
	}
	areas, err := h.areas.List(r.Context(), a)
	if err != nil {
		writeAreaError(w, err)
		return
	}
	items := make([]DrawnAreaResponse, 0, len(areas))
	for _, area := range areas {
		items = append(items, drawnAreaResponse(area, a.UserID))
	}
	response.WriteSuccess(w, http.StatusOK, DrawnAreaListResponse{Items: items})
}

// @Summary		Criar área desenhada
// @Description	Salva uma área desenhada no mapa (polígono de até 200 vértices sem cruzamentos, retângulo de quatro cantos alinhados, ou círculo de raio até 50 km), privada por padrão. O nome vem do cliente, proposto no idioma de quem desenhou. Use o `id` devolvido no filtro `{"field":"area","operator":"in","values":[id]}`; entram na área os endereços principais com posição que identifica a casa e também os com posição aproximada (CEP, bairro ou cidade) cujo ponto guardado fica dentro dela; com `"key":"exact_only"` no teste só entram os com posição de casa.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			body	body		lead.CreateDrawnAreaRequest	true	"Área"
// @Success		201		{object}	lead.DrawnAreaResponse
// @Failure		400		{object}	response.ErrorResponse	"invalid_body (corpo acima de 64 KiB incluso), area_name_required, area_name_too_long, area_visibility_invalid, area_shape_invalid, area_limit_reached"
// @Failure		403		{object}	response.ErrorResponse	"lead_addresses_forbidden"
// @Security		BearerAuth
// @Router			/lead-areas [post]
func (h *GeographyHandler) CreateArea(w http.ResponseWriter, r *http.Request) {
	a, ok := h.areaActor(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAreaBody)
	var body CreateDrawnAreaRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	area, err := h.areas.Create(r.Context(), a, body.toDomain())
	if err != nil {
		writeAreaError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, drawnAreaResponse(area, a.UserID))
}

// @Summary		Ver área desenhada
// @Description	Devolve uma área própria ou compartilhada. A área privada de outra pessoa responde 404.
// @Tags			Leads
// @Produce		json
// @Param			id	path		string	true	"ID da área"
// @Success		200	{object}	lead.DrawnAreaResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse	"area_not_found"
// @Security		BearerAuth
// @Router			/lead-areas/{id} [get]
func (h *GeographyHandler) GetArea(w http.ResponseWriter, r *http.Request) {
	a, ok := h.areaActor(w, r)
	if !ok {
		return
	}
	area, err := h.areas.Get(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeAreaError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, drawnAreaResponse(area, a.UserID))
}

// @Summary		Alterar área desenhada
// @Description	Renomeia, compartilha ou redesenha uma área; só quem a criou altera. As contagens em cache que usam a área passam a ser lidas de novo, porque a chave leva a hora da última alteração de cada área. O corpo aceita até 64 KiB.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"ID da área"
// @Param			body	body		lead.UpdateDrawnAreaRequest	true	"Campos a alterar"
// @Success		200		{object}	lead.DrawnAreaResponse
// @Failure		400		{object}	response.ErrorResponse	"invalid_body (corpo acima de 64 KiB incluso), area_name_required, area_name_too_long, area_visibility_invalid, area_shape_invalid"
// @Failure		403		{object}	response.ErrorResponse	"area_forbidden, lead_addresses_forbidden"
// @Failure		404		{object}	response.ErrorResponse	"area_not_found"
// @Security		BearerAuth
// @Router			/lead-areas/{id} [patch]
func (h *GeographyHandler) UpdateArea(w http.ResponseWriter, r *http.Request) {
	a, ok := h.areaActor(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAreaBody)
	var body UpdateDrawnAreaRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	area, err := h.areas.Update(r.Context(), a, mux.Vars(r)["id"], body.toDomain())
	if err != nil {
		writeAreaError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, drawnAreaResponse(area, a.UserID))
}

// @Summary		Apagar área desenhada
// @Description	Apaga uma área; só quem a criou apaga. Filtros e visões salvas que usam a área passam a responder 400 `area_not_found`.
// @Tags			Leads
// @Param			id	path	string	true	"ID da área"
// @Success		204
// @Failure		403	{object}	response.ErrorResponse	"area_forbidden, lead_addresses_forbidden"
// @Failure		404	{object}	response.ErrorResponse	"area_not_found"
// @Security		BearerAuth
// @Router			/lead-areas/{id} [delete]
func (h *GeographyHandler) DeleteArea(w http.ResponseWriter, r *http.Request) {
	a, ok := h.areaActor(w, r)
	if !ok {
		return
	}
	if err := h.areas.Delete(r.Context(), a, mux.Vars(r)["id"]); err != nil {
		writeAreaError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func mapPeekResponse(p lead_usecase.MapPeek, now time.Time) MapPeekResponse {
	items := make([]LeadRecordResponse, 0, len(p.Leads))
	for _, l := range p.Leads {
		items = append(items, toLeadRecord(l, now))
	}
	return MapPeekResponse{Total: p.Total, Items: items}
}
