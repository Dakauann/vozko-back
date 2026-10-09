package lead

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/crmfilter"
	leaddomain "vozko/domain/lead"
	"vozko/domain/leadmap"
	lead_usecase "vozko/usecases/lead"
)

type Sections interface {
	Summary(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter) (*leaddomain.SummarySection, error)
	FacetsColoredBy(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter, colorBy string) (*leaddomain.FacetsSection, error)
	Places(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter) (*leaddomain.PlacesSection, error)
	PlaceSuggestions(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter, prefix string) (*leaddomain.PlacesSection, error)
}

// @Summary		Resumo da página de leads
// @Description	Seção `summary` da página de leads, carregada quando o bloco entra na tela: total do filtro, com endereço, no mapa (posição que aponta a casa), aproximados (posição de bairro, CEP ou cidade), sem endereço, aniversariantes de hoje no fuso do workspace, bloqueados e janela de 24h aberta. Aceita os mesmos filtros de GET /leads. Fica em cache por 60 segundos por workspace, versão dos dados de leads, filtro e nível de acesso de quem pede; quando o limite de consultas analíticas simultâneas está ocupado, responde 503 com Retry-After. Um filtro em campo sensível sem `leads:read_sensitive` responde 403 (`custom_field_filter_sensitive_forbidden`); um filtro por CEP, precisão ou status do mapa ou área sem `leads:read_addresses` responde 403 (`lead_filter_address_forbidden`), porque bairro, cidade e UF continuam abertos mas o CEP aponta a rua; um filtro inválido responde 400 (`lead_filter_invalid`); um filtro de área responde como em GET /leads (`area_not_found`, `area_too_many`, `area_operator_unsupported`). A chave do cache leva a hora da última alteração de cada área do filtro.
// @Tags			Leads
// @Produce		json
// @Param			filter	query		string	false	"Filtro crmfilter em JSON (opcionalmente codificado em base64)"
// @Param			q		query		string	false	"Busca livre: cada palavra (2 caracteres ou mais, até 8, sem acento nem caixa) precisa aparecer no nome, apelido, bairro ou cidade do endereço principal ou numa memória; 4 dígitos ou mais buscam no número e nos telefones de contato. Sem palavra utilizável responde 400 lead_search_too_short"
// @Success		200		{object}	lead.SummarySection
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/sections/summary [get]
func (h *LeadHandler) SummarySection(w http.ResponseWriter, r *http.Request) {
	h.serveSection(w, r, func(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter) (any, error) {
		return h.sections.Summary(ctx, a, f)
	})
}

// @Summary		Facetas da página de leads
// @Description	Seção `facets` da página de leads: contagens do conjunto filtrado por canal, categoria de memória, status de envio em campanha, origem do lead e responsável (os 100 com mais leads, do maior para o menor, com o nome resolvido; `ownersTruncated` vem `true` quando o conjunto tem mais responsáveis do que a lista traz, e então a ausência de um responsável na lista não quer dizer zero leads) e, só para quem pode ler o campo, por valor do campo de classificação (`classification`, ausente quando não há campo visível). Com `colorBy`, `classification` conta os valores desse campo de seleção de lead, o mesmo que colore o mapa em GET /leads/map/layer: um campo que não é seleção de lead responde 400 (`map_color_by_invalid`) e um campo sensível sem `leads:read_sensitive` responde 403 (`map_color_by_forbidden`). Aceita os mesmos filtros de GET /leads. Cache, limites e recusas iguais aos de GET /leads/sections/summary.
// @Tags			Leads
// @Produce		json
// @Param			filter	query		string	false	"Filtro crmfilter em JSON (opcionalmente codificado em base64)"
// @Param			q		query		string	false	"Busca livre: cada palavra (2 caracteres ou mais, até 8, sem acento nem caixa) precisa aparecer no nome, apelido, bairro ou cidade do endereço principal ou numa memória; 4 dígitos ou mais buscam no número e nos telefones de contato. Sem palavra utilizável responde 400 lead_search_too_short"
// @Param			colorBy	query		string	false	"Chave do campo de seleção de lead cujos valores `classification` conta (o campo que colore o mapa)"
// @Success		200		{object}	lead.FacetsSection
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/sections/facets [get]
func (h *LeadHandler) FacetsSection(w http.ResponseWriter, r *http.Request) {
	colorBy := r.URL.Query().Get("colorBy")
	h.serveSection(w, r, func(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter) (any, error) {
		return h.sections.FacetsColoredBy(ctx, a, f, colorBy)
	})
}

// @Summary		Lugares da página de leads
// @Description	Seção `places` da página de leads: as 50 cidades e os 100 pares cidade e bairro com mais leads no endereço principal do conjunto filtrado, com a grafia mais comum. `pair` (`cityKey/districtKey`) é o valor do filtro `district`, e `cityKey` o do filtro `city`. Visível com `leads:read`, porque bairro e cidade não são endereço completo. Com `place`, vira a lista de sugestões da busca: até 8 cidades e 8 bairros cujo nome tem uma palavra que começa com o prefixo, comparado sem acento nem caixa (abreviações de bairro como Jd e Sto são expandidas); um prefixo com menos de 2 letras responde 400 lead_search_too_short. Aceita os mesmos filtros de GET /leads. Cache, limites e recusas iguais aos de GET /leads/sections/summary.
// @Tags			Leads
// @Produce		json
// @Param			filter	query		string	false	"Filtro crmfilter em JSON (opcionalmente codificado em base64)"
// @Param			q		query		string	false	"Busca livre: cada palavra (2 caracteres ou mais, até 8, sem acento nem caixa) precisa aparecer no nome, apelido, bairro ou cidade do endereço principal ou numa memória; 4 dígitos ou mais buscam no número e nos telefones de contato. Sem palavra utilizável responde 400 lead_search_too_short"
// @Param			place	query		string	false	"Prefixo para sugerir bairros e cidades (2 a 60 caracteres)"
// @Success		200		{object}	lead.PlacesSection
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Failure		504		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/sections/places [get]
func (h *LeadHandler) PlacesSection(w http.ResponseWriter, r *http.Request) {
	place := r.URL.Query().Get("place")
	h.serveSection(w, r, func(ctx context.Context, a lead_usecase.Actor, f crmfilter.Filter) (any, error) {
		if strings.TrimSpace(place) != "" {
			return h.sections.PlaceSuggestions(ctx, a, f, place)
		}
		return h.sections.Places(ctx, a, f)
	})
}

func (h *LeadHandler) serveSection(w http.ResponseWriter, r *http.Request, read func(context.Context, lead_usecase.Actor, crmfilter.Filter) (any, error)) {
	if h.sections == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "lead_sections_unavailable", "As seções de leads não estão disponíveis neste servidor", nil)
		return
	}
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	input, ok := h.listInput(w, r)
	if !ok {
		return
	}
	out, err := read(r.Context(), a, input.Filter)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		if httpx.WriteAnalyticsLimit(w, err, "leads") || writeColorByRefusal(w, err) {
			return
		}
		h.writeListError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func writeColorByRefusal(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, leadmap.ErrColorByForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, leadmap.ErrorCode(err), err.Error(), nil)
	case errors.Is(err, leadmap.ErrColorByInvalid):
		response.WriteErrorWithCode(w, http.StatusBadRequest, leadmap.ErrorCode(err), err.Error(), nil)
	default:
		return false
	}
	return true
}
