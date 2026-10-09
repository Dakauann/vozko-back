package lead

import (
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	leaddomain "vozko/domain/lead"
	"vozko/infra/http/middleware"
)

// @Summary		Listar leads
// @Description	Retorna a lista paginada de leads do workspace. Cada linha traz as colunas da página de leads: `owner` (responsável) e `ownerName` (o nome dele, resolvido por página), `phones` (telefones de contato), `primaryAddress` (o endereço principal; sem `leads:read_addresses` vêm só bairro, cidade, UF e código IBGE), `customFields` (campos sensíveis só com `leads:read_sensitive`; valores de campos que não existem mais nunca saem do servidor), `relativesCount` e `referredCount`. Aceita os filtros simples por querystring (nome, número, período, faixa etária, campanha, canal, bloqueio, janela, memórias) e/ou um filtro estruturado `filter` (crmfilter em JSON, opcionalmente em base64) com grupos AND/OR. Campos do filtro de leads: id, phone_any (identidade ou qualquer telefone de contato, nos dois formatos do nono dígito), number e q (também leem os telefones de contato), email, nickname, birthday (today, this_week de domingo a sábado ou this_month, no fuso do workspace), birth_date, owner, source, zip, state, city (chave da cidade), district (pares `cityKey/districtKey`, nunca só o bairro), geo_precision, geo_status, has_address, has_identity, opted_out, whatsapp_opt_in, relation_kind, relatives_count, referred_count, referred_by, custom (validado contra a definição do campo) e area (`in` com ids de áreas desenhadas, só com `leads:read_addresses`; por padrão entram os endereços com posição que identifica a casa e também os com posição aproximada, de CEP, bairro ou cidade, cujo ponto guardado fica dentro da área; com `"key":"exact_only"` no teste só entram os com posição que identifica a casa, e qualquer outra `key` responde 400 `lead_filter_invalid`; uma área apagada, de outro workspace ou privada de outra pessoa responde 400 `area_not_found`, mais de 20 testes de área no filtro 400 `area_too_many`), area_approximate (como area, mas lê só os endereços com posição aproximada, de CEP, bairro ou cidade, dentro da área: os leads que uma área `exact_only` deixou de fora; os testes de area e area_approximate somam no limite de 20) e geo_placement (in, not_in, eq ou neq com on_map, approximate, without_address, not_found, pending, quota_exceeded ou refused: as mesmas faixas e as mesmas contas de GET /leads/map/summary, para cada contagem virar um link). Um filtro em campo sensível sem `leads:read_sensitive` responde 403 (`custom_field_filter_sensitive_forbidden`); um filtro por zip, geo_precision, geo_status, geo_placement, area ou area_approximate sem `leads:read_addresses` responde 403 (`lead_filter_address_forbidden`); um filtro inválido responde 400 (`lead_filter_invalid`); uma busca sem palavra de 2 caracteres ou mais responde 400 (`lead_search_too_short`). city e district aceitam a chave em qualquer grafia (`SP:São Paulo/Jd. Paulista`) e comparam pela chave guardada. A ordenação aceita múltiplas chaves. A página lê primeiro os ids da página e depois os resumos só dessas linhas, e aceita até 200 itens por página.
// @Tags			Leads
// @Produce		json
// @Param			page				query	int		false	"Número da página (inicia em 1)"
// @Param			pageSize			query	int		false	"Itens por página (máximo 200)"
// @Param			sort				query	string	false	"Ordenação: createdAt, updatedAt, lastActivityAt, name, number, age, campaigns, memories, lastMemoryAt, relativesCount, referredCount (ex.: lastActivityAt:desc,name:asc)"
// @Param			order				query	string	false	"Direção padrão quando o sort não a informa ('asc' ou 'desc')"
// @Param			filter				query	string	false	"Filtro crmfilter em JSON (opcionalmente codificado em base64)"
// @Param			q					query	string	false	"Busca livre: cada palavra (2 caracteres ou mais, até 8, sem acento nem caixa; abaixo de 3 caracteres, só o início de uma palavra) precisa aparecer no nome, apelido, bairro ou cidade do endereço principal ou numa memória; 4 dígitos ou mais buscam no número e nos telefones de contato (um número inteiro, nos dois formatos do nono dígito). Sem palavra utilizável responde 400 lead_search_too_short. Sem sort escolhido, a busca ordena primeiro os nomes que começam com o texto, depois os mais parecidos e por fim os mais novos"
// @Param			number				query	string	false	"Filtrar por número de telefone"
// @Param			name				query	string	false	"Filtrar por nome"
// @Param			hasName				query	bool	false	"Possui nome preenchido"
// @Param			ageFrom				query	int		false	"Idade mínima"
// @Param			ageTo				query	int		false	"Idade máxima"
// @Param			blocked				query	bool	false	"Somente bloqueados / não bloqueados"
// @Param			windowOpen			query	bool	false	"Janela de 24h aberta"
// @Param			channel				query	[]string	false	"Canais (whatsapp, unofficial_whatsapp, telegram, instagram)"
// @Param			hasWhatsAppCampaign	query	bool	false	"Possui campanha de WhatsApp"
// @Param			campaignId			query	[]string	false	"IDs de campanha"
// @Param			campaignStatus		query	[]string	false	"Status de envio na campanha"
// @Param			campaignsFrom		query	int		false	"Mínimo de campanhas"
// @Param			campaignsTo			query	int		false	"Máximo de campanhas"
// @Param			stageId				query	[]string	false	"IDs de etapa do CRM"
// @Param			labelId				query	[]string	false	"IDs de etiqueta do CRM"
// @Param			hasMemory			query	bool	false	"Possui memórias registradas"
// @Param			memoryCategory		query	[]string	false	"Categorias de memória"
// @Param			memoryAuthor		query	[]string	false	"Autor da memória (human, ai, system)"
// @Param			memoryText			query	string	false	"Busca no conteúdo das memórias"
// @Param			memoriesFrom		query	int		false	"Mínimo de memórias"
// @Param			memoriesTo			query	int		false	"Máximo de memórias"
// @Param			memoryFrom			query	string	false	"Memória atualizada a partir de (RFC3339 ou YYYY-MM-DD)"
// @Param			memoryTo			query	string	false	"Memória atualizada até (RFC3339 ou YYYY-MM-DD)"
// @Param			createdFrom			query	string	false	"Criados a partir de (RFC3339 ou YYYY-MM-DD)"
// @Param			createdTo			query	string	false	"Criados até (RFC3339 ou YYYY-MM-DD)"
// @Param			updatedFrom			query	string	false	"Atualizados a partir de (RFC3339 ou YYYY-MM-DD)"
// @Param			updatedTo			query	string	false	"Atualizados até (RFC3339 ou YYYY-MM-DD)"
// @Param			activityFrom		query	string	false	"Última atividade a partir de (RFC3339 ou YYYY-MM-DD)"
// @Param			activityTo			query	string	false	"Última atividade até (RFC3339 ou YYYY-MM-DD)"
// @Success		200	{array}		lead.LeadListResponseItem
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads [get]
func (h *LeadHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.pages == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "lead_pages_unavailable", "A lista de leads não está disponível neste servidor", nil)
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

	result, err := h.pages.List(r.Context(), a, input)
	if err != nil {
		h.writeListError(w, err)
		return
	}

	items := make([]LeadListResponseItem, 0, len(result.Items))
	for _, lws := range result.Items {
		items = append(items, toLeadListItem(lws, h.now()))
	}

	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *LeadHandler) listInput(w http.ResponseWriter, r *http.Request) (leaddomain.ListLeadsInput, bool) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return leaddomain.ListLeadsInput{}, false
	}

	input, err := listInputFromQuery(workspaceID, r.URL.Query())
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid filter parameter", nil)
		return leaddomain.ListLeadsInput{}, false
	}
	return input, true
}

func (h *LeadHandler) writeListError(w http.ResponseWriter, err error) {
	if errors.Is(err, leaddomain.ErrLeadForbidden) {
		response.WriteErrorWithCode(w, http.StatusForbidden, leaddomain.ErrorCode(err), err.Error(), nil)
		return
	}
	if writeFilterRefusal(w, err) {
		return
	}
	response.WriteError(w, http.StatusInternalServerError, "Failed to fetch leads", nil)
}
