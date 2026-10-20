package whatsappformhttp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/whatsappform"
	whatsappform_usecase "vozko/usecases/whatsappform"
)

type Actor = conversation.Viewer

type Service interface {
	Options(ctx context.Context, a Actor) (whatsappform_usecase.Options, error)
	Create(ctx context.Context, a Actor, in whatsappform_usecase.CreateInput) (whatsappform_usecase.FormDetail, error)
	List(ctx context.Context, a Actor, search string, archived bool) ([]whatsappform.FormSummary, error)
	Get(ctx context.Context, a Actor, formID string) (whatsappform_usecase.FormDetail, error)
	Rename(ctx context.Context, a Actor, formID, name, description string) (whatsappform.Form, error)
	SaveDraft(ctx context.Context, a Actor, formID string, doc whatsappform.Document, baseVersionID string) (whatsappform.Version, error)
	Check(ctx context.Context, a Actor, doc whatsappform.Document) (whatsappform.Problems, error)
	Compiled(ctx context.Context, a Actor, formID string) ([]byte, error)
	Versions(ctx context.Context, a Actor, formID string) ([]whatsappform.Version, error)
	Duplicate(ctx context.Context, a Actor, formID string) (whatsappform_usecase.FormDetail, error)
	Archive(ctx context.Context, a Actor, formID string) error
	Publish(ctx context.Context, a Actor, formID string) (whatsappform_usecase.PublishResult, error)
	Deprecate(ctx context.Context, a Actor, formID, metaWABAID string) error
	Preview(ctx context.Context, a Actor, formID string) (whatsappform_usecase.Preview, error)
	Sendable(ctx context.Context, a Actor, q whatsappform_usecase.SendableQuery) ([]whatsappform_usecase.SendableForm, error)
	SendInConversation(ctx context.Context, a Actor, in whatsappform_usecase.SendInput) (whatsappform.Send, error)
	Sends(ctx context.Context, a Actor, formID, after string, limit int) (whatsappform.Page[whatsappform.Send], error)
	ListResponses(ctx context.Context, a Actor, q whatsappform.ResponseQuery) (whatsappform.Page[whatsappform_usecase.ResponseRow], error)
	GetResponse(ctx context.Context, a Actor, responseID string) (whatsappform_usecase.ResponseDetail, error)
	DeleteResponse(ctx context.Context, a Actor, responseID string) error
	Stats(ctx context.Context, a Actor, formID string, p whatsappform_usecase.Period) (whatsappform.Stats, error)
	Distribution(ctx context.Context, a Actor, formID, fieldKey string, p whatsappform_usecase.Period) (map[string]int64, error)
	Automations(ctx context.Context, a Actor, formID string) ([]whatsappform_usecase.FormAutomation, error)
	ScaffoldAutomation(ctx context.Context, a Actor, formID string) (string, error)
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (Actor, bool) {
	a, ok := httpx.WorkspaceActor(r)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
	}
	return a, ok
}

func formID(r *http.Request) string {
	return mux.Vars(r)["formId"]
}

func refuseQuery(w http.ResponseWriter) {
	writeError(w, whatsappform.ErrListQueryInvalid)
}

func dateBound(values url.Values, key string, endOfDay bool) (*time.Time, bool) {
	raw := strings.TrimSpace(values.Get(key))
	if raw == "" {
		return nil, true
	}
	at := shared.ParseDateBound(raw, endOfDay)
	return at, at != nil
}

func period(values url.Values) (whatsappform_usecase.Period, bool) {
	from, okFrom := dateBound(values, "from", false)
	to, okTo := dateBound(values, "to", true)
	return whatsappform_usecase.Period{From: from, To: to}, okFrom && okTo
}

func limit(values url.Values) (int, bool) {
	raw := strings.TrimSpace(values.Get("limit"))
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	return n, err == nil
}

// @Summary		Opções do construtor de formulários
// @Description	Tipos de campo, blocos de exibição, categorias da Meta, os limites de texto e de quantidade que a Meta aplica, onde cada tipo de campo pode ser salvo no lead (inclusive os campos personalizados do workspace, sem os sensíveis) e os modelos prontos. O construtor usa só estes dados: nenhuma regra é repetida no navegador. Exige `whatsapp_forms:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Success		200	{object}	whatsappformhttp.OptionsResponse
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Security		BearerAuth
// @Router			/whatsapp/forms/options [get]
func (h *Handler) Options(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	options, err := h.service.Options(r.Context(), a)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toOptions(options))
}

// @Summary		Listar formulários
// @Description	Formulários do workspace com onde estão publicados (uma linha por conta do WhatsApp), quantos envios e respostas tiveram e quando chegou a última resposta. `archived=true` inclui os arquivados. Exige `whatsapp_forms:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			search		query		string	false	"Parte do nome"
// @Param			archived	query		bool	false	"Incluir arquivados"
// @Success		200			{object}	whatsappformhttp.FormListResponse
// @Failure		403			{object}	response.ErrorResponse	"forbidden"
// @Security		BearerAuth
// @Router			/whatsapp/forms [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	archived := httpx.ParseBoolQuery(r.URL.Query().Get("archived"))
	forms, err := h.service.List(r.Context(), a, r.URL.Query().Get("search"), archived != nil && *archived)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, FormListResponse{Items: orEmpty(forms)})
}

// @Summary		Criar formulário
// @Description	Cria um formulário em branco ou a partir de um modelo pronto (`starter`). O primeiro rascunho já vem conferido: `draft.problems` lista o que falta corrigir antes de publicar. Exige `whatsapp_forms:create` e `whatsapp_forms:update`.
// @Tags			Formulários do WhatsApp
// @Accept			json
// @Produce		json
// @Param			request	body		CreateRequest	true	"Nome e modelo"
// @Success		201		{object}	whatsappformhttp.FormDetailResponse
// @Failure		400		{object}	response.ErrorResponse	"whatsapp_form_name_required, whatsapp_form_name_too_long, whatsapp_form_starter_unknown"
// @Failure		403		{object}	response.ErrorResponse	"forbidden"
// @Failure		409		{object}	response.ErrorResponse	"whatsapp_form_name_taken"
// @Security		BearerAuth
// @Router			/whatsapp/forms [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CreateRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	detail, err := h.service.Create(r.Context(), a, whatsappform_usecase.CreateInput{Name: req.Name, Description: req.Description, Starter: req.Starter})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toDetail(detail))
}

// @Summary		Ver formulário
// @Description	O formulário, o rascunho atual com os problemas a corrigir, a versão publicada e a publicação em cada conta do WhatsApp com o status da Meta. Exige `whatsapp_forms:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário (UUID)"
// @Success		200		{object}	whatsappformhttp.FormDetailResponse
// @Failure		403		{object}	response.ErrorResponse	"forbidden"
// @Failure		404		{object}	response.ErrorResponse	"whatsapp_form_not_found"
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	detail, err := h.service.Get(r.Context(), a, formID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toDetail(detail))
}

// @Summary		Renomear formulário
// @Description	Troca o nome e a descrição. O nome na Meta não muda: os modelos de mensagem continuam apontando para o mesmo formulário. Exige `whatsapp_forms:update`.
// @Tags			Formulários do WhatsApp
// @Accept			json
// @Produce		json
// @Param			formId	path		string			true	"ID do formulário (UUID)"
// @Param			request	body		RenameRequest	true	"Nome e descrição"
// @Success		200		{object}	whatsappform.Form
// @Failure		400		{object}	response.ErrorResponse	"whatsapp_form_name_required, whatsapp_form_description_too_long"
// @Failure		409		{object}	response.ErrorResponse	"whatsapp_form_name_taken, whatsapp_form_archived"
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId} [patch]
func (h *Handler) Rename(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req RenameRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	form, err := h.service.Rename(r.Context(), a, formID(r), req.Name, req.Description)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, form)
}

// @Summary		Salvar rascunho
// @Description	Salva uma nova versão do rascunho. `baseVersionId` é o rascunho que o construtor abriu: se outra pessoa salvou depois, a resposta é 409 `whatsapp_form_stale_draft` e nada é perdido. Problemas nunca impedem salvar; eles voltam em `draft.problems`. Um documento igual ao atual não cria versão. Exige `whatsapp_forms:update`.
// @Tags			Formulários do WhatsApp
// @Accept			json
// @Produce		json
// @Param			formId	path		string			true	"ID do formulário (UUID)"
// @Param			request	body		DraftRequest	true	"Documento e versão de base"
// @Success		200		{object}	whatsappformhttp.DraftResponse
// @Failure		403		{object}	response.ErrorResponse	"forbidden"
// @Failure		409		{object}	response.ErrorResponse	"whatsapp_form_stale_draft, whatsapp_form_archived"
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/draft [put]
func (h *Handler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req DraftRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	draft, err := h.service.SaveDraft(r.Context(), a, formID(r), req.Document, req.BaseVersionID)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, DraftResponse{Draft: draft})
}

// @Summary		Conferir documento
// @Description	Confere um documento sem salvar: regras e limites da Meta, rotas entre telas, condições de exibição e onde cada resposta é salva no lead. Cada problema traz a tela, o bloco e a propriedade para o construtor marcar. Exige `whatsapp_forms:read`.
// @Tags			Formulários do WhatsApp
// @Accept			json
// @Produce		json
// @Param			request	body		CheckRequest	true	"Documento"
// @Success		200		{object}	whatsappformhttp.ProblemsResponse
// @Security		BearerAuth
// @Router			/whatsapp/forms/check [post]
func (h *Handler) Check(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CheckRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	problems, err := h.service.Check(r.Context(), a, req.Document)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, ProblemsResponse{Problems: orEmpty(problems)})
}

// @Summary		Ver o JSON do rascunho
// @Description	O Flow JSON que o rascunho gera para a Meta, só para leitura. 422 com os problemas quando o rascunho ainda não pode ser gerado. Exige `whatsapp_forms:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId	path	string	true	"ID do formulário (UUID)"
// @Success		200
// @Failure		422	{object}	response.ErrorResponse	"whatsapp_form_invalid"
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/compiled [get]
func (h *Handler) Compiled(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	raw, err := h.service.Compiled(r.Context(), a, formID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// @Summary		Versões do formulário
// @Description	Todas as versões salvas, da mais nova para a mais antiga. As respostas apontam para a versão que foi preenchida. Exige `whatsapp_forms:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário (UUID)"
// @Success		200		{object}	whatsappformhttp.VersionsResponse
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/versions [get]
func (h *Handler) Versions(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	versions, err := h.service.Versions(r.Context(), a, formID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, VersionsResponse{Items: orEmpty(versions)})
}

// @Summary		Duplicar formulário
// @Description	Cria um formulário novo com uma cópia do rascunho, chamado "(cópia)". Exige `whatsapp_forms:create` e `whatsapp_forms:update`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário (UUID)"
// @Success		201		{object}	whatsappformhttp.FormDetailResponse
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/duplicate [post]
func (h *Handler) Duplicate(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	detail, err := h.service.Duplicate(r.Context(), a, formID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toDetail(detail))
}

// @Summary		Arquivar formulário
// @Description	Descontinua o formulário na Meta em todas as contas e então arquiva. Se a Meta não confirmar em alguma conta, nada é arquivado. As respostas continuam guardadas. Exige `whatsapp_forms:delete`.
// @Tags			Formulários do WhatsApp
// @Param			formId	path	string	true	"ID do formulário (UUID)"
// @Success		204
// @Failure		403	{object}	response.ErrorResponse	"forbidden, whatsapp_form_not_owner"
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/archive [post]
func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.service.Archive(r.Context(), a, formID(r)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Publicar formulário
// @Description	Publica o rascunho em todas as contas do WhatsApp onde o workspace é dono de um número oficial conectado. Uma conta que já tem o formulário recebe a nova versão no mesmo formulário da Meta, então os modelos de mensagem continuam funcionando. `outcomes` traz o resultado de cada conta; uma conta com problema não impede as outras. 422 com os problemas quando o rascunho tem algo a corrigir (nada é enviado à Meta). Exige `whatsapp_forms:configure`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário (UUID)"
// @Success		200		{object}	whatsappformhttp.PublishResponse
// @Failure		409		{object}	response.ErrorResponse	"whatsapp_form_no_account, whatsapp_form_archived"
// @Failure		422		{object}	response.ErrorResponse	"whatsapp_form_invalid"
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/publish [post]
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	result, err := h.service.Publish(r.Context(), a, formID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPublish(result))
}

// @Summary		Descontinuar em uma conta
// @Description	Descontinua o formulário na Meta em uma conta do WhatsApp. A Meta não permite reverter; publicar de novo cria um formulário novo nessa conta. Exige `whatsapp_forms:configure`.
// @Tags			Formulários do WhatsApp
// @Param			formId		path	string	true	"ID do formulário (UUID)"
// @Param			metaWabaId	path	string	true	"ID da conta do WhatsApp na Meta"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse	"whatsapp_form_publication_not_found"
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/publications/{metaWabaId}/deprecate [post]
func (h *Handler) Deprecate(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.service.Deprecate(r.Context(), a, formID(r), mux.Vars(r)["metaWabaId"]); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Pré-visualizar na Meta
// @Description	Gera a pré-visualização oficial da Meta para o rascunho, sem mexer no formulário publicado. O link vale por até 30 dias e pode ser aberto num iframe. Exige `whatsapp_forms:update`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário (UUID)"
// @Success		200		{object}	whatsappformhttp.PreviewResponse
// @Failure		409		{object}	response.ErrorResponse	"whatsapp_form_no_account"
// @Failure		422		{object}	response.ErrorResponse	"whatsapp_form_invalid"
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/preview [post]
func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	preview, err := h.service.Preview(r.Context(), a, formID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, PreviewResponse{PreviewURL: preview.URL, ExpiresAt: preview.ExpiresAt})
}

// @Summary		Formulários que um número pode enviar
// @Description	Formulários publicados e liberados pela Meta na conta do número (`phoneId`) ou da conta (`metaWabaId`); informe só um dos dois. `metaFlowId` e `firstScreen` são o que um botão de formulário num modelo de mensagem usa. Exige `whatsapp_forms:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			phoneId		query		string	false	"ID do número oficial"
// @Param			metaWabaId	query		string	false	"ID da conta do WhatsApp na Meta"
// @Success		200			{object}	whatsappformhttp.SendableResponse
// @Failure		403			{object}	response.ErrorResponse	"whatsapp_form_not_owner"
// @Security		BearerAuth
// @Router			/whatsapp/forms/sendable [get]
func (h *Handler) Sendable(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := whatsappform_usecase.SendableQuery{PhoneID: strings.TrimSpace(r.URL.Query().Get("phoneId")), MetaWABAID: strings.TrimSpace(r.URL.Query().Get("metaWabaId"))}
	if (q.PhoneID == "") == (q.MetaWABAID == "") {
		refuseQuery(w)
		return
	}
	forms, err := h.service.Sendable(r.Context(), a, q)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toSendable(forms))
}

// @Summary		Enviar formulário na conversa
// @Description	Envia o formulário na conversa do WhatsApp oficial, com o texto e o botão informados. Só funciona com a janela de 24 horas aberta: fora dela a resposta é 409 `whatsapp_form_outside_window`, e o caminho é um modelo de mensagem com botão de formulário. Exige `whatsapp_forms:send` e acesso à conversa.
// @Tags			Formulários do WhatsApp
// @Accept			json
// @Produce		json
// @Param			entryId	path		string		true	"ID da conversa do WhatsApp oficial (UUID)"
// @Param			request	body		SendRequest	true	"Formulário e textos"
// @Success		201		{object}	whatsappform.Send
// @Failure		400		{object}	response.ErrorResponse	"whatsapp_form_copy_invalid"
// @Failure		403		{object}	response.ErrorResponse	"forbidden, whatsapp_form_not_owner"
// @Failure		409		{object}	response.ErrorResponse	"whatsapp_form_outside_window, whatsapp_form_not_published, whatsapp_form_not_sendable, whatsapp_form_provider_unsupported"
// @Security		BearerAuth
// @Router			/conversations/whatsapp/{entryId}/forms [post]
func (h *Handler) Send(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req SendRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	send, err := h.service.SendInConversation(r.Context(), a, whatsappform_usecase.SendInput{EntryID: mux.Vars(r)["entryId"], FormID: req.FormID, Copy: req.Copy})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, send)
}

// @Summary		Envios do formulário
// @Description	Cada envio do formulário, do mais recente para o mais antigo, com a origem, o status e o motivo da falha. Respeita os departamentos de quem consulta. Paginado por cursor (`after`). Exige `whatsapp_forms:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário (UUID)"
// @Param			after	query		string	false	"Cursor da próxima página"
// @Param			limit	query		int		false	"Itens por página (até 100)"
// @Success		200		{object}	whatsappformhttp.SendsResponse
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/sends [get]
func (h *Handler) Sends(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	n, okLimit := limit(r.URL.Query())
	if !okLimit {
		refuseQuery(w)
		return
	}
	page, err := h.service.Sends(r.Context(), a, formID(r), r.URL.Query().Get("after"), n)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, SendsResponse{Items: orEmpty(page.Items), Next: page.Next})
}

// @Summary		Respostas do formulário
// @Description	Respostas recebidas, da mais recente para a mais antiga, só das conversas que quem consulta alcança. Respostas sensíveis, fotos e documentos aparecem como •••• sem `whatsapp_form_responses:read_sensitive`. `match=unmatched` traz respostas que não vieram de um envio deste workspace (por exemplo, um formulário enviado pelo WhatsApp Manager). Datas inclusivas (YYYY-MM-DD ou RFC3339). Paginado por cursor (`after`). Exige `whatsapp_form_responses:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId		path		string	true	"ID do formulário (UUID)"
// @Param			from		query		string	false	"Recebidas a partir de"
// @Param			to			query		string	false	"Recebidas até"
// @Param			origin		query		string	false	"Origens separadas por vírgula"	Enums(inbox, template_start, template_scheduled, template_bulk, template, campaign, workflow, elo)
// @Param			phoneId		query		string	false	"IDs de números, separados por vírgula"
// @Param			versionId	query		string	false	"IDs de versões, separados por vírgula"
// @Param			campaignId	query		string	false	"ID da campanha"
// @Param			match		query		string	false	"Correspondência"	Enums(matched, unmatched)
// @Param			search		query		string	false	"Nome do lead ou número"
// @Param			after		query		string	false	"Cursor da próxima página"
// @Param			limit		query		int		false	"Itens por página (até 100)"
// @Success		200			{object}	whatsappformhttp.ResponsesResponse
// @Failure		400			{object}	response.ErrorResponse	"whatsapp_form_list_invalid"
// @Failure		403			{object}	response.ErrorResponse	"forbidden"
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/responses [get]
func (h *Handler) Responses(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	p, okPeriod := period(values)
	n, okLimit := limit(values)
	if !okPeriod || !okLimit {
		refuseQuery(w)
		return
	}
	q := whatsappform.ResponseQuery{
		FormID: formID(r), From: p.From, To: p.To, VersionIDs: httpx.ParseCSVQuery(values["versionId"]),
		PhoneIDs: httpx.ParseCSVQuery(values["phoneId"]), CampaignID: strings.TrimSpace(values.Get("campaignId")),
		Match: whatsappform.MatchFilter(strings.TrimSpace(values.Get("match"))), Search: values.Get("search"),
		After: values.Get("after"), Limit: n,
	}
	for _, origin := range httpx.ParseCSVQuery(values["origin"]) {
		q.Origins = append(q.Origins, whatsappform.SendOrigin(origin))
	}
	page, err := h.service.ListResponses(r.Context(), a, q)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponses(page))
}

// @Summary		Ver resposta
// @Description	A resposta com a versão do formulário preenchida, o lead e o que foi salvo nele. `revealed` diz se as respostas sensíveis vieram abertas. Exige `whatsapp_form_responses:read` e alcance da conversa.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			responseId	path		string	true	"ID da resposta (UUID)"
// @Success		200			{object}	whatsappformhttp.ResponseDetailResponse
// @Failure		404			{object}	response.ErrorResponse	"whatsapp_form_response_not_found"
// @Security		BearerAuth
// @Router			/whatsapp/forms/responses/{responseId} [get]
func (h *Handler) Response(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	detail, err := h.service.GetResponse(r.Context(), a, mux.Vars(r)["responseId"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponseDetail(detail))
}

// @Summary		Excluir resposta
// @Description	Exclui a resposta. Exige `whatsapp_form_responses:delete` e alcance da conversa.
// @Tags			Formulários do WhatsApp
// @Param			responseId	path	string	true	"ID da resposta (UUID)"
// @Success		204
// @Failure		403	{object}	response.ErrorResponse	"forbidden"
// @Failure		404	{object}	response.ErrorResponse	"whatsapp_form_response_not_found"
// @Security		BearerAuth
// @Router			/whatsapp/forms/responses/{responseId} [delete]
func (h *Handler) DeleteResponse(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.service.DeleteResponse(r.Context(), a, mux.Vars(r)["responseId"]); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Desempenho do formulário
// @Description	Envios por status, entregues, lidos, respostas, tempo mediano até a resposta, por origem e por dia, no período. Formulários sem servidor não informam em qual tela a pessoa parou. Exige `whatsapp_form_responses:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário (UUID)"
// @Param			from	query		string	false	"Início do período"
// @Param			to		query		string	false	"Fim do período"
// @Success		200		{object}	whatsappform.Stats
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/stats [get]
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	p, okPeriod := period(r.URL.Query())
	if !okPeriod {
		refuseQuery(w)
		return
	}
	stats, err := h.service.Stats(r.Context(), a, formID(r), p)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, stats)
}

// @Summary		Distribuição de um campo de escolha
// @Description	Quantas respostas escolheram cada opção do campo, no período. Exige `whatsapp_form_responses:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId		path		string	true	"ID do formulário (UUID)"
// @Param			fieldKey	path		string	true	"Chave do campo"
// @Param			from		query		string	false	"Início do período"
// @Param			to			query		string	false	"Fim do período"
// @Success		200			{object}	whatsappformhttp.DistributionResponse
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/fields/{fieldKey}/distribution [get]
func (h *Handler) Distribution(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	p, okPeriod := period(r.URL.Query())
	if !okPeriod {
		refuseQuery(w)
		return
	}
	counts, err := h.service.Distribution(r.Context(), a, formID(r), mux.Vars(r)["fieldKey"], p)
	if err != nil {
		writeError(w, err)
		return
	}
	if counts == nil {
		counts = map[string]int64{}
	}
	response.WriteSuccess(w, http.StatusOK, DistributionResponse{Counts: counts})
}

// @Summary		Automações do formulário
// @Description	Fluxos de automação que começam quando o formulário é respondido ou que enviam o formulário. Exige `whatsapp_forms:read`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário (UUID)"
// @Success		200		{object}	whatsappformhttp.AutomationsResponse
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/automations [get]
func (h *Handler) Automations(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	items, err := h.service.Automations(r.Context(), a, formID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toAutomations(items))
}

// @Summary		Criar automação para o formulário
// @Description	Cria um fluxo de automação desligado, já ligado ao formulário: quando ele é respondido, adiciona uma etiqueta, cria um negócio no funil padrão e avisa o responsável. Você edita e liga no editor de fluxos. Exige `whatsapp_forms:read` e `workflows:create`.
// @Tags			Formulários do WhatsApp
// @Produce		json
// @Param			formId	path		string	true	"ID do formulário (UUID)"
// @Success		201		{object}	whatsappformhttp.AutomationCreatedResponse
// @Failure		403		{object}	response.ErrorResponse	"forbidden"
// @Security		BearerAuth
// @Router			/whatsapp/forms/{formId}/automations [post]
func (h *Handler) ScaffoldAutomation(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	id, err := h.service.ScaffoldAutomation(r.Context(), a, formID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, AutomationCreatedResponse{WorkflowID: id})
}
