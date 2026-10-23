package campaignaudiencehttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/audiencex"
	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/campaignaudience"
	"vozko/domain/emaildesign"
	"vozko/domain/leadmap"
	"vozko/domain/whatsapp/template"
	"vozko/domain/workspace"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

const (
	uploadOverhead  = 1 << 20
	codeBodyInvalid = "audience_request_invalid"
	codeDocument    = "email_design_malformed"
	codeTemplate    = "template_not_found"
	pathChannel     = "channel"
	pathList        = "listId"
	pathRow         = "rowId"
	pathTemplate    = "id"
)

type API interface {
	Count(ctx context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, params campaignaudience_usecase.Params, spec campaignaudience.Spec) (campaignaudience.Count, error)
	Leads(ctx context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, params campaignaudience_usecase.Params, spec campaignaudience.Spec, q campaignaudience_usecase.LeadsQuery) (campaignaudience_usecase.LeadsPage, error)
	Map(ctx context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, spec campaignaudience.Spec) (leadmap.Sample, error)
	Upload(ctx context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, up campaignaudience_usecase.Upload, view campaignaudience_usecase.View) (campaignaudience_usecase.Preview, error)
	Preview(ctx context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, listID string, view campaignaudience_usecase.View) (campaignaudience_usecase.Preview, error)
	Rows(ctx context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, listID string, q campaignaudience_usecase.RowsQuery) (campaignaudience_usecase.RowPage, error)
	AddRow(ctx context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, listID string, in campaignaudience_usecase.RowInput) (campaignaudience.Row, error)
	EditRow(ctx context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, listID, rowID string, in campaignaudience_usecase.RowInput) (campaignaudience.Row, error)
	RemoveRow(ctx context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, listID, rowID string) error
	Sample(ctx context.Context, a campaignaudience_usecase.Actor, ch campaignaudience.Channel, params campaignaudience_usecase.Params, vars []campaignaudience.Variable, format campaignaudience_usecase.SampleFormat) (campaignaudience_usecase.File, error)
	Fields(ctx context.Context, a campaignaudience_usecase.Actor) ([]campaignaudience_usecase.Field, error)
	TemplateSuggestions(ctx context.Context, a campaignaudience_usecase.Actor, templateID string) (campaignaudience_usecase.TemplateSuggestions, error)
	SaveTemplateSuggestions(ctx context.Context, a campaignaudience_usecase.Actor, templateID string, suggestions []campaignaudience.Suggestion) (campaignaudience_usecase.TemplateSuggestions, error)
}

type Handler struct {
	api API
}

func NewHandler(api API) *Handler {
	return &Handler{api: api}
}

func actor(w http.ResponseWriter, r *http.Request) (campaignaudience_usecase.Actor, bool) {
	a, ok := httpx.WorkspaceActor(r)
	if !ok {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "workspace_required", "Workspace context is required", nil)
	}
	return a, ok
}

func channelOf(r *http.Request) campaignaudience.Channel {
	return campaignaudience.Channel(mux.Vars(r)[pathChannel])
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, codeBodyInvalid, "Corpo da requisição inválido", nil)
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if audiencex.WriteRefusal(w, err) {
		return
	}
	if errors.Is(err, emaildesign.ErrMalformedDocument) || errors.Is(err, emaildesign.ErrUnknownBlockKind) {
		response.WriteErrorWithCode(w, http.StatusBadRequest, codeDocument, err.Error(), nil)
		return
	}
	log.Printf("[campaign-audience] request failed: %v", err)
	response.WriteError(w, http.StatusInternalServerError, "Falha ao montar o público", nil)
}

// @Summary		Contar o público de uma campanha
// @Description	Conta quem recebe antes de a campanha existir, pelas mesmas regras do envio do canal. A origem é `leads` (uma seleção de leads, como na tela de Leads) ou `list` (o `listId` de uma lista enviada). Cada variável liga a um campo do lead; no WhatsApp, quando algum lead não tem o valor, a escolha entre `text` (texto padrão) e `exclude` (deixar de fora) é obrigatória e, enquanto falta, `sendable` vem nulo com o bloqueio `variable_choice_required`. No e-mail as variáveis são as tags do assunto e do documento, e sem escolha vale o texto padrão da própria tag. `out` conta cada destinatário uma vez, pelo primeiro motivo; `variables[].missing` conta cada lead sem o valor em cada variável. Campos por canal: whatsapp `templateId` (e `businessPhoneId` para contar o intervalo entre disparos), unofficial `message` (e `instanceId`), email `subject` e `document`, carrier `kind`. No WhatsApp o modelo (ou a mensagem) é opcional na contagem: sem ele a contagem já traz encontrados, com WhatsApp, quem recebe e quem fica de fora pelos motivos que não dependem do modelo (sem número, bloqueado, opt-out, outra campanha em andamento e, com `businessPhoneId`, o intervalo entre disparos), sem variáveis, com o bloqueio `template_required` (ou `message_required` no não oficial) até a escolha. Com o modelo, `variables` traz uma variável por parâmetro na ordem do envio: a do texto do cabeçalho primeiro (`header: true`), depois as do corpo; `name` é o nome da variável (`nome` nas nomeadas, `1`, `2` nas numeradas) e `key` é a posição. No WhatsApp, `workflowId` e `agentId` são o fluxo e o agente da campanha: cada destinatário precisa ter, nos metadados, as chaves que eles pedem (a mesma regra da criação). `metadataMissing` lista cada chave faltando com quantos destinatários não a têm e, numa lista, as linhas (até 50 por chave); numa lista o bloqueio é `list_metadata_missing`, e com leads, que não têm metadados, é `leads_metadata_unavailable`. Exige a ação `create` do recurso do canal e, para leads, `leads:read`.
// @Tags			Campaign audience
// @Accept			json
// @Produce		json
// @Param			channel	path		string											true	"Canal"	Enums(email, whatsapp, unofficial, carrier)
// @Param			body	body		campaignaudiencehttp.CountRequest				true	"Público e campos do canal"
// @Success		200		{object}	campaignaudience.Count
// @Failure		400		{object}	response.ErrorResponse	"audience_source_invalid, audience_source_ambiguous, audience_variable_unknown, audience_text_required, audience_template_required, audience_message_required, audience_kind_required, selection_*, send_binding_*"
// @Failure		403		{object}	response.ErrorResponse	"forbidden, campaign_workflow_forbidden"
// @Failure		404		{object}	response.ErrorResponse	"audience_list_not_found"
// @Failure		410		{object}	response.ErrorResponse	"audience_list_expired"
// @Failure		422		{object}	response.ErrorResponse	"audience_list_channel_mismatch, audience_source_unsupported, send_template_unavailable, send_instance_unavailable, campaign_workflow_not_found, campaign_agent_not_found"
// @Failure		503		{object}	response.ErrorResponse	"audience_busy, audience_unavailable, campaign_automation_unavailable"
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/count [post]
func (h *Handler) Count(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	var req CountRequest
	if !decode(w, r, &req) {
		return
	}
	params, err := req.params()
	if err != nil {
		writeError(w, err)
		return
	}
	count, err := h.api.Count(r.Context(), a, channelOf(r), params, req.Spec())
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, count)
}

// @Summary		Listar os leads do público
// @Description	Os destinatários da contagem, um a um, pela mesma passada e as mesmas regras de `POST /campaign-audience/{channel}/count` (mesmo corpo: público e campos do canal). Cada item traz `leadId` (vazio para uma linha de lista que ainda não é lead, com `line`), `name`, `contact` (telefone ou e-mail do canal), `status` (`ready` recebe, `out` fica de fora, `undecided` espera a escolha de uma variável), `reason` (o motivo de quem fica de fora, a mesma chave de `out` na contagem, como `no_email`, `no_identity`, `blocked`, `opted_out`, `already_in_running_campaign`, `suppressed` ou `missing_variable`), `missing` (as chaves das variáveis sem valor no lead, as mesmas contadas em `variables[].missing`), `metadataMissing` (as chaves de metadados que o fluxo ou o agente pede e faltam) e `variables` (os valores que serão enviados, ou os do lead quando ele fica de fora). Filtros: `status`, `reason`, `variable` (chave da variável), `metadataKey` e `search` (nome ou contato); o total de cada filtro bate com o número da contagem. Para escolher leads um a um, envie a seleção `{"mode": "ids", "ids": [...]}` (até 5.000) na contagem, nesta lista e na criação; seleção por ids não precisa de `expectedCount` nem `fingerprint`. `limit` vai de 1 a 200. Exige a ação `create` do recurso do canal e, para leads, `leads:read`.
// @Tags			Campaign audience
// @Accept			json
// @Produce		json
// @Param			channel	path		string									true	"Canal"	Enums(email, whatsapp, unofficial, carrier)
// @Param			body	body		campaignaudiencehttp.LeadSearchRequest	true	"Público, campos do canal, filtros e página"
// @Success		200		{array}		campaignaudience.AudienceLead
// @Failure		400		{object}	response.ErrorResponse	"audience_page_invalid, audience_lead_status_invalid, audience_source_invalid, audience_variable_unknown, selection_*"
// @Failure		403		{object}	response.ErrorResponse	"forbidden, campaign_workflow_forbidden"
// @Failure		404		{object}	response.ErrorResponse	"audience_list_not_found"
// @Failure		410		{object}	response.ErrorResponse	"audience_list_expired"
// @Failure		422		{object}	response.ErrorResponse	"audience_list_channel_mismatch, audience_source_unsupported, send_template_unavailable, send_instance_unavailable, campaign_workflow_not_found, campaign_agent_not_found"
// @Failure		503		{object}	response.ErrorResponse	"audience_busy, audience_unavailable, campaign_automation_unavailable"
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/leads [post]
func (h *Handler) Leads(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	var req LeadSearchRequest
	if !decode(w, r, &req) {
		return
	}
	params, err := req.params()
	if err != nil {
		writeError(w, err)
		return
	}
	q := req.query()
	page, err := h.api.Leads(r.Context(), a, channelOf(r), params, req.Spec(), q)
	if err != nil {
		writePageError(w, err)
		return
	}
	writePage(w, page.Items, page.Page, page.Limit, page.Total)
}

// @Summary		Mapa do público de uma campanha
// @Description	As posições dos leads do público, pela mesma seleção de `POST /campaign-audience/{channel}/count` (mesmo `source` e `selection`; só a origem `leads`), para a prévia do mapa no passo de público. O servidor decide quem está em cada área desenhada do filtro, como na contagem. `layer` vem no formato dos pontos de `GET /leads/map/tiles/{z}/{x}/{y}`: um ponto por posição e classe (`placement` `on_map` ou `approximate`), com `count` pessoas e até 5 `leadIds`. `located` conta os leads com posição, `positions` as posições distintas e, quando passam de 1.000, `sampled` é verdadeiro e `layer` traz uma amostra estável de 1.000 posições. `bounds` é a caixa que junta as áreas desenhadas do filtro (nula sem área). Pedidos iguais ao mesmo tempo fazem uma só leitura. Exige a ação `create` do recurso do canal, `leads:read` e `leads:read_addresses`.
// @Tags			Campaign audience
// @Accept			json
// @Produce		json
// @Param			channel	path		string							true	"Canal"	Enums(email, whatsapp, unofficial, carrier)
// @Param			body	body		campaignaudiencehttp.MapRequest	true	"Público"
// @Success		200		{object}	campaignaudiencehttp.MapResponse
// @Failure		400		{object}	response.ErrorResponse	"audience_source_invalid, audience_source_ambiguous, audience_selection_required, selection_*"
// @Failure		403		{object}	response.ErrorResponse	"forbidden"
// @Failure		422		{object}	response.ErrorResponse	"audience_source_unsupported"
// @Failure		503		{object}	response.ErrorResponse	"audience_busy, audience_unavailable"
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/map [post]
func (h *Handler) Map(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	var req MapRequest
	if !decode(w, r, &req) {
		return
	}
	sample, err := h.api.Map(r.Context(), a, channelOf(r), req.Spec())
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, mapResponse(sample))
}

// @Summary		Enviar a lista de uma campanha
// @Description	Lê no servidor uma planilha CSV, TSV, TXT ou XLSX (primeira aba) de até 20 MB e 150 000 linhas, ou um texto colado, e guarda a lista por 24 horas para a criação da campanha. A primeira linha é cabeçalho quando nomeia alguma coluna conhecida (telefone, number, numero, phone, celular, whatsapp; nome, name; var1, variavel1; e-mail; ou o campo do lead sugerido para uma variável, como primeiro_nome ou bairro), senão a lista é lida por posição. Nenhum lead é criado aqui: `existing` diz quantas linhas já são leads e `new` quantos leads serão criados na criação. Com `templateId` (whatsapp) ou `message` (unofficial) em `context`, `columns.variables` diz qual coluna preenche cada variável. Multipart: `file` e, opcional, `context` (JSON de campaignaudiencehttp.ListContext). JSON: campaignaudiencehttp.UploadTextRequest (`text` vazio cria uma lista vazia para incluir contatos à mão). Exige a ação `create` do recurso do canal.
// @Tags			Campaign audience
// @Accept			multipart/form-data
// @Accept			json
// @Produce		json
// @Param			channel	path		string										true	"Canal"	Enums(email, whatsapp, unofficial)
// @Param			file	formData	file										false	"Planilha"
// @Param			context	formData	string										false	"JSON de campaignaudiencehttp.ListContext"
// @Param			body	body		campaignaudiencehttp.UploadTextRequest		false	"Texto colado ou lista vazia"
// @Success		201		{object}	campaignaudience_usecase.Preview
// @Failure		400		{object}	response.ErrorResponse	"audience_file_required, audience_file_unreadable, audience_file_empty, audience_request_invalid"
// @Failure		413		{object}	response.ErrorResponse	"audience_file_too_large, audience_list_too_large, audience_file_too_wide"
// @Failure		422		{object}	response.ErrorResponse	"audience_contact_column_missing, audience_source_unsupported"
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/lists [post]
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	up, view, ok := uploadOf(w, r)
	if !ok {
		return
	}
	preview, err := h.api.Upload(r.Context(), a, channelOf(r), up, view)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, preview)
}

func uploadOf(w http.ResponseWriter, r *http.Request) (campaignaudience_usecase.Upload, campaignaudience_usecase.View, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, campaignaudience.MaxFileBytes+uploadOverhead)
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "multipart/form-data" {
		var req UploadTextRequest
		if !decode(w, r, &req) {
			return campaignaudience_usecase.Upload{}, campaignaudience_usecase.View{}, false
		}
		view, err := req.view()
		if err != nil {
			writeError(w, err)
			return campaignaudience_usecase.Upload{}, campaignaudience_usecase.View{}, false
		}
		up := campaignaudience_usecase.Upload{FileName: req.FileName, Data: []byte(req.Text), Empty: strings.TrimSpace(req.Text) == ""}
		return up, view, true
	}
	if err := r.ParseMultipartForm(campaignaudience.MaxFileBytes + uploadOverhead); err != nil {
		writeError(w, campaignaudience.ErrFileTooLarge)
		return campaignaudience_usecase.Upload{}, campaignaudience_usecase.View{}, false
	}
	var view campaignaudience_usecase.View
	if raw := strings.TrimSpace(r.FormValue("context")); raw != "" {
		var c ListContext
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			response.WriteErrorWithCode(w, http.StatusBadRequest, codeBodyInvalid, "Contexto da lista inválido", nil)
			return campaignaudience_usecase.Upload{}, campaignaudience_usecase.View{}, false
		}
		v, err := c.view()
		if err != nil {
			writeError(w, err)
			return campaignaudience_usecase.Upload{}, campaignaudience_usecase.View{}, false
		}
		view = v
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, campaignaudience.ErrFileRequired)
		return campaignaudience_usecase.Upload{}, campaignaudience_usecase.View{}, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, campaignaudience.MaxFileBytes+1))
	if err != nil {
		writeError(w, campaignaudience.ErrFileUnreadable)
		return campaignaudience_usecase.Upload{}, campaignaudience_usecase.View{}, false
	}
	return campaignaudience_usecase.Upload{FileName: header.Filename, Data: data}, view, true
}

// @Summary		Ver a prévia de uma lista enviada
// @Description	Refaz a prévia de uma lista do próprio membro: linhas, já leads e leads novos, linhas puladas com o motivo e qual coluna preenche cada variável do modelo ou da mensagem do `context`. Com `workflowId` ou `agentId` no `context`, `metadataMissing` lista cada chave de metadados que o fluxo ou o agente pede e que falta, com quantas linhas e quais (até 50 por chave), pela mesma regra da criação. Exige a ação `create` do recurso do canal.
// @Tags			Campaign audience
// @Accept			json
// @Produce		json
// @Param			channel	path		string								true	"Canal"	Enums(email, whatsapp, unofficial)
// @Param			listId	path		string								true	"Lista"
// @Param			body	body		campaignaudiencehttp.ListContext	false	"Modelo ou mensagem"
// @Success		200		{object}	campaignaudience_usecase.Preview
// @Failure		403		{object}	response.ErrorResponse	"campaign_workflow_forbidden"
// @Failure		404		{object}	response.ErrorResponse	"audience_list_not_found"
// @Failure		410		{object}	response.ErrorResponse	"audience_list_expired"
// @Failure		422		{object}	response.ErrorResponse	"audience_list_channel_mismatch, campaign_workflow_not_found, campaign_agent_not_found"
// @Failure		503		{object}	response.ErrorResponse	"campaign_automation_unavailable"
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/lists/{listId}/preview [post]
func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	var c ListContext
	if r.ContentLength != 0 && !decode(w, r, &c) {
		return
	}
	view, err := c.view()
	if err != nil {
		writeError(w, err)
		return
	}
	preview, err := h.api.Preview(r.Context(), a, channelOf(r), mux.Vars(r)[pathList], view)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, preview)
}

// @Summary		Listar as linhas de uma lista
// @Description	Página das linhas, na ordem da planilha (as incluídas à mão vêm antes), com as variáveis já preenchidas pelo servidor (coluna, nome da linha ou campo do lead) e, em `missing`, os números das variáveis que ainda faltam; com `workflowId` ou `agentId`, `metadataMissing` traz as chaves de metadados que o fluxo ou o agente pede e a linha não tem. `missingOnly` mantém só as linhas com variável ou chave faltando. `limit` vai de 1 a 200. Exige a ação `create` do recurso do canal.
// @Tags			Campaign audience
// @Accept			json
// @Produce		json
// @Param			channel	path		string								true	"Canal"	Enums(email, whatsapp, unofficial)
// @Param			listId	path		string								true	"Lista"
// @Param			body	body		campaignaudiencehttp.RowSearchRequest	true	"Busca e página"
// @Success		200		{array}		campaignaudience_usecase.RowView
// @Failure		400		{object}	response.ErrorResponse	"audience_page_invalid"
// @Failure		404		{object}	response.ErrorResponse	"audience_list_not_found"
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/lists/{listId}/rows/search [post]
func (h *Handler) Rows(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	var req RowSearchRequest
	if !decode(w, r, &req) {
		return
	}
	view, err := req.view()
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := h.api.Rows(r.Context(), a, channelOf(r), mux.Vars(r)[pathList], campaignaudience_usecase.RowsQuery{View: view, Search: req.Search, MissingOnly: req.MissingOnly, Page: req.Page, Limit: req.Limit})
	if err != nil {
		writePageError(w, err)
		return
	}
	writePage(w, page.Items, page.Page, page.Limit, page.Total)
}

const CodePageInvalid = "audience_page_invalid"

func writePage(w http.ResponseWriter, items any, page, limit, total int) {
	totalPages := 0
	if limit > 0 {
		totalPages = (total + limit - 1) / limit
	}
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{Page: page, PageSize: limit, TotalPages: totalPages, TotalItems: int64(total)})
}

func writePageError(w http.ResponseWriter, err error) {
	if errors.Is(err, campaignaudience_usecase.ErrPageInvalid) {
		response.WriteErrorWithCode(w, http.StatusBadRequest, CodePageInvalid, err.Error(), nil)
		return
	}
	writeError(w, err)
}

// @Summary		Incluir um contato na lista
// @Description	Inclui à mão uma linha na lista do próprio membro. O telefone pode vir em qualquer formato (ou o e-mail, no canal de e-mail); `variables` e `metadata` são os valores da linha. Renova o prazo da lista. Exige a ação `create` do recurso do canal.
// @Tags			Campaign audience
// @Accept			json
// @Produce		json
// @Param			channel	path		string							true	"Canal"	Enums(email, whatsapp, unofficial)
// @Param			listId	path		string							true	"Lista"
// @Param			body	body		campaignaudiencehttp.RowRequest	true	"Contato"
// @Success		201		{object}	campaignaudiencehttp.RowResponse
// @Failure		400		{object}	response.ErrorResponse	"audience_row_contact_invalid"
// @Failure		404		{object}	response.ErrorResponse	"audience_list_not_found"
// @Failure		409		{object}	response.ErrorResponse	"audience_row_duplicate"
// @Failure		413		{object}	response.ErrorResponse	"audience_list_too_large"
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/lists/{listId}/rows [post]
func (h *Handler) AddRow(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	var req RowRequest
	if !decode(w, r, &req) {
		return
	}
	row, err := h.api.AddRow(r.Context(), a, channelOf(r), mux.Vars(r)[pathList], req.input())
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, rowResponse(row))
}

// @Summary		Editar um contato da lista
// @Description	Troca contato, nome, variáveis e metadados de uma linha; `metadata` ausente mantém os da planilha. Renova o prazo da lista. Exige a ação `create` do recurso do canal.
// @Tags			Campaign audience
// @Accept			json
// @Produce		json
// @Param			channel	path		string							true	"Canal"	Enums(email, whatsapp, unofficial)
// @Param			listId	path		string							true	"Lista"
// @Param			rowId	path		string							true	"Linha"
// @Param			body	body		campaignaudiencehttp.RowRequest	true	"Contato"
// @Success		200		{object}	campaignaudiencehttp.RowResponse
// @Failure		400		{object}	response.ErrorResponse	"audience_row_contact_invalid"
// @Failure		404		{object}	response.ErrorResponse	"audience_list_not_found, audience_row_not_found"
// @Failure		409		{object}	response.ErrorResponse	"audience_row_duplicate"
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/lists/{listId}/rows/{rowId} [patch]
func (h *Handler) EditRow(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	var req RowRequest
	if !decode(w, r, &req) {
		return
	}
	vars := mux.Vars(r)
	row, err := h.api.EditRow(r.Context(), a, channelOf(r), vars[pathList], vars[pathRow], req.input())
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, rowResponse(row))
}

// @Summary		Remover um contato da lista
// @Description	Remove uma linha da lista do próprio membro e renova o prazo da lista. Exige a ação `create` do recurso do canal.
// @Tags			Campaign audience
// @Produce		json
// @Param			channel	path		string	true	"Canal"	Enums(email, whatsapp, unofficial)
// @Param			listId	path		string	true	"Lista"
// @Param			rowId	path		string	true	"Linha"
// @Success		200		{object}	campaignaudiencehttp.DeletedResponse
// @Failure		404		{object}	response.ErrorResponse	"audience_list_not_found, audience_row_not_found"
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/lists/{listId}/rows/{rowId} [delete]
func (h *Handler) RemoveRow(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	vars := mux.Vars(r)
	if err := h.api.RemoveRow(r.Context(), a, channelOf(r), vars[pathList], vars[pathRow]); err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, DeletedResponse{ID: vars[pathRow]})
}

// @Summary		Baixar o modelo da lista
// @Description	Gera a planilha modelo do canal. Telefone: `number;name` e uma coluna por variável do modelo (`templateId`) ou da mensagem (`message`); a variável com campo do lead sugerido usa o nome do campo (como `primeiro_nome` ou `bairro`) e a variável livre usa `varN`. A linha de exemplo usa os exemplos do modelo. E-mail: `email;name`. `format` é `csv` (padrão, separado por ponto e vírgula) ou `xlsx`. Exige a ação `create` do recurso do canal.
// @Tags			Campaign audience
// @Accept			json
// @Produce		text/csv
// @Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Param			channel	path		string								true	"Canal"	Enums(email, whatsapp, unofficial)
// @Param			body	body		campaignaudiencehttp.SampleRequest	false	"Modelo ou mensagem e formato"
// @Success		200		{file}		file
// @Failure		400		{object}	response.ErrorResponse	"audience_sample_format_invalid, audience_template_required, audience_message_required"
// @Failure		422		{object}	response.ErrorResponse	"send_template_unavailable, audience_source_unsupported"
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/sample [post]
func (h *Handler) Sample(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	var req SampleRequest
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	params, err := req.params()
	if err != nil {
		writeError(w, err)
		return
	}
	file, err := h.api.Sample(r.Context(), a, channelOf(r), params, audiencex.Variables(req.Variables), campaignaudience_usecase.SampleFormat(strings.TrimSpace(req.Format)))
	if errors.Is(err, campaignaudience_usecase.ErrSampleFormat) {
		response.WriteErrorWithCode(w, http.StatusBadRequest, CodeSampleFormat, err.Error(), nil)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", file.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+file.Name+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file.Data)
}

const CodeSampleFormat = "audience_sample_format_invalid"

// @Summary		Listar os campos do lead que uma variável pode usar
// @Description	Os campos fixos (primeiro nome, nome completo, apelido, bairro, cidade, responsável) e os campos personalizados de lead do workspace que não são sensíveis, com a coluna que os representa na planilha. Os nomes dos campos fixos são traduzidos no front pela `source`. A mesma lista serve o passo de público (com a ação `read` do recurso do canal) e o editor de modelos do WhatsApp (`GET /campaign-audience/template-fields`, com `whatsapp_templates:read`).
// @Tags			Campaign audience
// @Produce		json
// @Param			channel	path		string	true	"Canal"	Enums(email, whatsapp, unofficial, carrier)
// @Success		200		{array}		campaignaudience_usecase.Field
// @Security		BearerAuth
// @Router			/campaign-audience/{channel}/fields [get]
func (h *Handler) Fields(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	fields, err := h.api.Fields(r.Context(), a)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, fields)
}

// @Summary		Listar os campos do lead para variáveis de modelo
// @Description	A mesma lista de `GET /campaign-audience/{channel}/fields`, para o editor de modelos do WhatsApp. Exige `whatsapp_templates:read`.
// @Tags			Campaign audience
// @Produce		json
// @Success		200		{array}		campaignaudience_usecase.Field
// @Security		BearerAuth
// @Router			/campaign-audience/template-fields [get]
func (h *Handler) TemplateFields(w http.ResponseWriter, r *http.Request) {
	h.Fields(w, r)
}

// @Summary		Ver os campos sugeridos para as variáveis de um modelo
// @Description	O campo do lead sugerido para cada variável do modelo, guardado só na plataforma (nunca vai para a Meta). O passo de público usa a sugestão como ligação padrão e o modelo da lista nomeia a coluna pelo campo. Variável livre não tem entrada. Exige `whatsapp_templates:read`.
// @Tags			Campaign audience
// @Produce		json
// @Param			id	path		string	true	"Modelo"
// @Success		200	{object}	campaignaudience_usecase.TemplateSuggestions
// @Failure		404	{object}	response.ErrorResponse	"template_not_found"
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id}/variable-suggestions [get]
func (h *Handler) Suggestions(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	out, err := h.api.TemplateSuggestions(r.Context(), a, mux.Vars(r)[pathTemplate])
	if err != nil {
		writeSuggestionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Salvar os campos sugeridos para as variáveis de um modelo
// @Description	Troca todas as sugestões do modelo neste workspace. Cada `slot` (1 a N, o número da variável) recebe um campo do lead (`lead.first_name`, `lead.name`, `lead.nickname`, `lead.district`, `lead.city`, `lead.owner_name` ou `lead.custom:<chave>`); campo sensível, inexistente ou texto fixo é recusado. O `slot` segue a ordem do envio: a variável do texto do cabeçalho primeiro, depois as do corpo, nomeadas ou numeradas. Exige `whatsapp_templates:update`.
// @Tags			Campaign audience
// @Accept			json
// @Produce		json
// @Param			id		path		string									true	"Modelo"
// @Param			body	body		campaignaudiencehttp.SuggestionsRequest	true	"Sugestões"
// @Success		200		{object}	campaignaudience_usecase.TemplateSuggestions
// @Failure		400		{object}	response.ErrorResponse	"send_binding_unknown, send_binding_field_unknown, send_binding_sensitive, template_suggestion_literal, template_suggestion_duplicate"
// @Failure		404		{object}	response.ErrorResponse	"template_not_found"
// @Failure		422		{object}	response.ErrorResponse	"template_suggestion_slot_invalid"
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id}/variable-suggestions [put]
func (h *Handler) SaveSuggestions(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	var req SuggestionsRequest
	if !decode(w, r, &req) {
		return
	}
	out, err := h.api.SaveTemplateSuggestions(r.Context(), a, mux.Vars(r)[pathTemplate], req.Suggestions)
	if err != nil {
		writeSuggestionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func writeSuggestionError(w http.ResponseWriter, err error) {
	if errors.Is(err, template.ErrTemplateNotFound) || errors.Is(err, template.ErrTemplateAccessDenied) {
		response.WriteErrorWithCode(w, http.StatusNotFound, codeTemplate, "Modelo não encontrado", nil)
		return
	}
	writeError(w, err)
}

type AccessControl func(workspace.Resource, workspace.Action, http.HandlerFunc) http.HandlerFunc

var channelResources = []struct {
	channel  campaignaudience.Channel
	resource workspace.Resource
}{
	{campaignaudience.ChannelEmail, workspace.ResourceEmailMarketing},
	{campaignaudience.ChannelWhatsApp, workspace.ResourceWhatsAppCampaigns},
	{campaignaudience.ChannelUnofficial, workspace.ResourceUnofficialWhatsAppCampaigns},
	{campaignaudience.ChannelCarrier, workspace.ResourceCarrierMessaging},
}

func RegisterRoutes(protected *mux.Router, h *Handler, ac AccessControl) {
	if h == nil || ac == nil {
		return
	}
	protected.HandleFunc("/campaign-audience/template-fields", ac(workspace.ResourceWhatsAppTemplates, workspace.ActionRead, h.TemplateFields)).Methods(http.MethodGet)
	for _, c := range channelResources {
		base := "/campaign-audience/{" + pathChannel + ":" + string(c.channel) + "}"
		create := func(f http.HandlerFunc) http.HandlerFunc { return ac(c.resource, workspace.ActionCreate, f) }
		protected.HandleFunc(base+"/count", create(h.Count)).Methods(http.MethodPost)
		protected.HandleFunc(base+"/leads", create(h.Leads)).Methods(http.MethodPost)
		protected.HandleFunc(base+"/map", create(h.Map)).Methods(http.MethodPost)
		protected.HandleFunc(base+"/lists", create(h.Upload)).Methods(http.MethodPost)
		protected.HandleFunc(base+"/lists/{"+pathList+"}/preview", create(h.Preview)).Methods(http.MethodPost)
		protected.HandleFunc(base+"/lists/{"+pathList+"}/rows/search", create(h.Rows)).Methods(http.MethodPost)
		protected.HandleFunc(base+"/lists/{"+pathList+"}/rows", create(h.AddRow)).Methods(http.MethodPost)
		protected.HandleFunc(base+"/lists/{"+pathList+"}/rows/{"+pathRow+"}", create(h.EditRow)).Methods(http.MethodPatch)
		protected.HandleFunc(base+"/lists/{"+pathList+"}/rows/{"+pathRow+"}", create(h.RemoveRow)).Methods(http.MethodDelete)
		protected.HandleFunc(base+"/sample", create(h.Sample)).Methods(http.MethodPost)
		protected.HandleFunc(base+"/fields", ac(c.resource, workspace.ActionRead, h.Fields)).Methods(http.MethodGet)
	}
	wt := workspace.ResourceWhatsAppTemplates
	protected.HandleFunc("/whatsapp/templates/{"+pathTemplate+"}/variable-suggestions", ac(wt, workspace.ActionRead, h.Suggestions)).Methods(http.MethodGet)
	protected.HandleFunc("/whatsapp/templates/{"+pathTemplate+"}/variable-suggestions", ac(wt, workspace.ActionUpdate, h.SaveSuggestions)).Methods(http.MethodPut)
}

func Codes() []string {
	return append(audiencex.Codes(), codeBodyInvalid, codeTemplate, CodePageInvalid, CodeSampleFormat, codeDocument)
}
