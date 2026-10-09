package lead

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	advertisinghttp "vozko/delivery/http/advertising"
	calllisthttp "vozko/delivery/http/calllist"
	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	"vozko/domain/campaign"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	leaddomain "vozko/domain/lead"
	"vozko/domain/leadaction"
	"vozko/domain/report"
	"vozko/domain/selection"
	"vozko/infra/http/middleware"
	lead_usecase "vozko/usecases/lead"
	leadaction_usecase "vozko/usecases/leadaction"
	report_usecase "vozko/usecases/report"
)

const (
	runIDPath      = "/{runId:" + uuidPattern + "}"
	previewIDPath  = "/{previewId:" + uuidPattern + "}"
	audienceIDPath = "/{audienceId:" + uuidPattern + "}"
)

type Actions interface {
	Start(ctx context.Context, req leadaction_usecase.Request) (leadaction_usecase.Outcome, error)
	Preview(ctx context.Context, req leadaction_usecase.Request) (*leadaction.Preview, error)
	PreviewStatus(ctx context.Context, a lead_usecase.Actor, id string) (*leadaction.Preview, error)
	Run(ctx context.Context, a lead_usecase.Actor, id string) (*leadaction.Run, error)
	Audience(ctx context.Context, a lead_usecase.Actor, id string) (*leadaction.AudienceJob, error)
}

type LeadActionParams struct {
	Key             string                     `json:"key,omitempty" example:"classificacao"`
	Value           json.RawMessage            `json:"value,omitempty" swaggertype:"object"`
	OwnerID         *string                    `json:"ownerId,omitempty" example:"4b1c8a52-0d9e-4f6a-9c3e-2a7d5b8e1f00"`
	Blocked         *bool                      `json:"blocked,omitempty"`
	BusinessPhoneID string                     `json:"businessPhoneId,omitempty"`
	Format          string                     `json:"format,omitempty" example:"csv"`
	Addresses       bool                       `json:"addresses,omitempty"`
	Sensitive       bool                       `json:"sensitive,omitempty"`
	AdAccountID     string                     `json:"adAccountId,omitempty"`
	Name            string                     `json:"name,omitempty"`
	Description     string                     `json:"description,omitempty"`
	CallList        *leadaction.CallListParams `json:"callList,omitempty"`
	Send            *LeadSendParams            `json:"send,omitempty"`
}

type LeadSelectionRequest struct {
	Mode          string            `json:"mode" example:"all_matching"`
	IDs           []string          `json:"ids,omitempty"`
	Filter        *crmfilter.Filter `json:"filter,omitempty"`
	Sort          []crmfilter.Sort  `json:"sort,omitempty"`
	Limit         int               `json:"limit,omitempty"`
	ExcludeIDs    []string          `json:"excludeIds,omitempty"`
	ExpectedCount int               `json:"expectedCount,omitempty"`
	Fingerprint   string            `json:"fingerprint,omitempty"`
}

type LeadActionRequest struct {
	Action    string               `json:"action" example:"classify"`
	Params    LeadActionParams     `json:"params"`
	Selection LeadSelectionRequest `json:"selection"`
}

type LeadActionRunResponse struct {
	ID          string            `json:"id"`
	Action      string            `json:"action"`
	Status      string            `json:"status" example:"running"`
	Phase       string            `json:"phase" example:"edit"`
	ActorID     string            `json:"actorId"`
	Params      leadaction.Params `json:"params"`
	Result      leadaction.Result `json:"result"`
	FailureCode string            `json:"failureCode,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	StartedAt   *time.Time        `json:"startedAt,omitempty"`
	FinishedAt  *time.Time        `json:"finishedAt,omitempty"`
}

type LeadAudienceResponse struct {
	ID          string                `json:"id"`
	Status      string                `json:"status" example:"pending"`
	Audience    *advertising.Audience `json:"audience,omitempty"`
	Matched     int                   `json:"matched"`
	Skipped     int                   `json:"skipped"`
	FailureCode string                `json:"failureCode,omitempty"`
	StartedAt   time.Time             `json:"startedAt"`
	UpdatedAt   time.Time             `json:"updatedAt"`
}

type ActionStartResponse struct {
	Action   string                         `json:"action"`
	Run      *LeadActionRunResponse         `json:"run,omitempty"`
	Report   *report.Job                    `json:"report,omitempty"`
	Audience *LeadAudienceResponse          `json:"audience,omitempty"`
	CallList *calllisthttp.CallListResponse `json:"callList,omitempty"`
	Send     *campaign.SendReview           `json:"send,omitempty"`
}

type ActionPreviewResponse struct {
	ID          string                   `json:"id"`
	Action      string                   `json:"action"`
	Status      string                   `json:"status" example:"done"`
	Result      leadaction.PreviewResult `json:"result"`
	FailureCode string                   `json:"failureCode,omitempty"`
	UpdatedAt   time.Time                `json:"updatedAt"`
	Send        *campaign.SendQuote      `json:"send,omitempty"`
}

type ActionSelectionChangedResponse struct {
	Error    bool   `json:"error" example:"true"`
	Code     string `json:"code" example:"selection_changed"`
	Message  string `json:"message"`
	Expected int    `json:"expected" example:"340"`
	Matched  int    `json:"matched" example:"352"`
}

func (req LeadActionRequest) toUseCase(a lead_usecase.Actor, r *http.Request) leadaction_usecase.Request {
	p := req.Params
	return leadaction_usecase.Request{
		Actor:            a,
		DepartmentID:     middleware.SelectedDepartmentID(r),
		DepartmentFilter: middleware.GetDepartmentFilter(r),
		Action:           leadaction.Action(strings.TrimSpace(req.Action)),
		Params: leadaction.Params{
			Key: p.Key, Value: p.Value, OwnerID: p.OwnerID, Blocked: p.Blocked, BusinessPhoneID: p.BusinessPhoneID,
			Format: p.Format, Addresses: p.Addresses, Sensitive: p.Sensitive,
			AdAccountID: p.AdAccountID, Name: p.Name, Description: p.Description, CallList: p.CallList, Send: p.Send.toDomain(),
		},
		Selection: selection.Selection{
			Mode: selection.Mode(strings.TrimSpace(req.Selection.Mode)), IDs: req.Selection.IDs, Filter: req.Selection.Filter,
			Sort: req.Selection.Sort, Limit: req.Selection.Limit, ExcludeIDs: req.Selection.ExcludeIDs,
			ExpectedCount: req.Selection.ExpectedCount, Fingerprint: req.Selection.Fingerprint,
		},
		IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
		Locale:         httpx.RequestLocale(r),
	}
}

func runResponse(run *leadaction.Run) *LeadActionRunResponse {
	if run == nil {
		return nil
	}
	return &LeadActionRunResponse{
		ID: run.ID, Action: string(run.Action), Status: string(run.Status), Phase: string(run.Phase), ActorID: run.ActorID,
		Params: run.Params, Result: run.Result, FailureCode: string(run.FailureCode),
		CreatedAt: run.CreatedAt, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
}

func audienceResponse(j *leadaction.AudienceJob) *LeadAudienceResponse {
	if j == nil {
		return nil
	}
	return &LeadAudienceResponse{
		ID: j.ID, Status: string(j.Status), Audience: j.Audience, Matched: j.Matched, Skipped: j.Skipped,
		FailureCode: j.FailureCode, StartedAt: j.StartedAt, UpdatedAt: j.UpdatedAt,
	}
}

func previewResponse(p *leadaction.Preview) ActionPreviewResponse {
	return ActionPreviewResponse{ID: p.ID, Action: string(p.Action), Status: string(p.Status), Result: p.Result, FailureCode: p.FailureCode, UpdatedAt: p.UpdatedAt, Send: p.Send}
}

func (h *LeadHandler) actionsReady(w http.ResponseWriter) bool {
	if h.actions == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, leadaction.ErrorCode(leadaction.ErrUnavailable), "As ações em massa sobre leads não estão disponíveis neste servidor", nil)
		return false
	}
	return true
}

func (h *LeadHandler) actionRequest(w http.ResponseWriter, r *http.Request) (leadaction_usecase.Request, bool) {
	if !h.actionsReady(w) {
		return leadaction_usecase.Request{}, false
	}
	a, ok := h.requestActor(w, r)
	if !ok {
		return leadaction_usecase.Request{}, false
	}
	var req LeadActionRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return leadaction_usecase.Request{}, false
	}
	return req.toUseCase(a, r), true
}

// @Summary		Prévia de uma ação em massa sobre leads
// @Description	Conta a seleção (sem cache) e quantos leads a ação mudaria, por motivo de pulo, lendo a seleção em blocos de 5.000 ids. Ações: `classify` (params `key` e `value`; `null` limpa o campo; campo sensível exige `leads:read_sensitive`), `assign_owner` (`ownerId`, vazio remove o responsável), `block` (`blocked` e, opcional, `businessPhoneId` para bloquear também no WhatsApp), `export` (`format` csv, `addresses` e `sensitive` para as colunas de endereço completo e de campos sensíveis) e `meta_audience` (`adAccountId`, `name`, `description`; não aceita o modo first_n e recusa filtro em campo sensível). A seleção tem um modo: `ids` (até 5.000 escolhidos), `all_matching` (filtro não vazio, o mesmo filtro efetivo da lista, com a busca incluída), `first_n` (os primeiros `limit` pela ordem `sort`, com exclusões e, numa edição, os leads que já têm o valor pulados antes do limite) ou `everyone` (todos os leads do workspace). Não precisa de `fingerprint` nem de `expectedCount`: a resposta traz `fingerprint` e `expectedCount` para enviar em POST /leads/actions. Para `export` e `meta_audience` a prévia só conta a seleção uma vez (`selected`, sem leads bloqueados no público). Um bloqueio com `businessPhoneId` de número que o workspace não pode usar responde 422 `lead_action_phone_unavailable`. Responde 200 quando termina em até 3 segundos; senão responde 202 com a prévia em andamento e o resultado parcial, e o restante continua em segundo plano (consulte GET /leads/actions/previews/{previewId}). Exige as permissões da ação: `leads.bulk_edit` para classificar, `leads.bulk_edit` e `leads.assign` para atribuir, `leads.bulk_edit` e `leads.block` para bloquear, `leads.export` (mais `leads.read_addresses` e `leads.read_sensitive` conforme as colunas) para exportar e `leads.meta_audience` para o público e `call_lists.manage` para a lista de ligação. `send_template` e `send_unofficial` (params `send`: `name`, `departmentId` numa conta com departamentos, `split` para dividir acima de 150.000 leads, `bindings` com uma ligação por variável, cada uma `{source, value}` com `source` `literal` (e `value`), `lead.first_name`, `lead.name`, `lead.nickname`, `lead.district`, `lead.city`, `lead.owner_name` ou `lead.custom:<chave>` de campo não sensível; no oficial `businessPhoneId` e `templateId`, sem variável no cabeçalho nem variáveis nomeadas; no não oficial `instanceId`, `message`, `sendDelayMinMs`, `sendDelayMaxMs` e `dailyCap`) contam a seleção uma vez e respondem `send` com a cotação dos selecionados: `parts`, `splitRequired`, custo em micros de US$ com a moeda do saldo, `capRemaining`, `fits` e `refusal`, ou `dailyCap` e `estimatedDays` no não oficial; os motivos de pulo aparecem na revisão do disparo preparado. Exigem `leads.send_template` ou `leads.send_unofficial`. Quando o limite de consultas analíticas está ocupado, responde 503 com Retry-After.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			body	body		LeadActionRequest	true	"Ação, parâmetros e seleção"
// @Success		200		{object}	ActionPreviewResponse
// @Success		202		{object}	ActionPreviewResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		413		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/actions/preview [post]
func (h *LeadHandler) PreviewAction(w http.ResponseWriter, r *http.Request) {
	req, ok := h.actionRequest(w, r)
	if !ok {
		return
	}
	p, err := h.actions.Preview(httpx.WithCreationScope(r, "").Context(), req)
	if err != nil {
		writeActionError(w, err)
		return
	}
	status := http.StatusOK
	if p.Status == leadaction.PreviewRunning {
		status = http.StatusAccepted
	}
	response.WriteSuccess(w, status, previewResponse(p))
}

// @Summary		Andamento de uma prévia de ação sobre leads
// @Description	Devolve a prévia que POST /leads/actions/preview deixou em segundo plano: `running` com o resultado parcial, `done` com as contagens finais ou `failed` com `failureCode`. Só quem pediu a prévia a lê; ela expira em 15 minutos.
// @Tags			Leads
// @Produce		json
// @Param			previewId	path		string	true	"ID da prévia"
// @Success		200			{object}	ActionPreviewResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/actions/previews/{previewId} [get]
func (h *LeadHandler) ActionPreview(w http.ResponseWriter, r *http.Request) {
	if !h.actionsReady(w) {
		return
	}
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	p, err := h.actions.PreviewStatus(r.Context(), a, mux.Vars(r)["previewId"])
	if err != nil {
		writeActionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, previewResponse(p))
}

// @Summary		Executar uma ação em massa sobre leads
// @Description	Executa a ação sobre a seleção confirmada. O cabeçalho `Idempotency-Key` (até 128 caracteres) é obrigatório: a mesma chave com o mesmo pedido devolve o mesmo resultado sem refazer nada, e com outro pedido responde 409 `idempotency_key_reused`. `fingerprint` e `expectedCount` vêm da prévia (ou da contagem da lista) e são obrigatórios fora do modo `ids` (sem a contagem responde 400 `selection_count_required`); se a contagem mudou, responde 409 `selection_changed` com a contagem nova. `classify`, `assign_owner` e `block` congelam a seleção e respondem 202 com a execução (`run`), aplicada em lotes de 500 com versão nova e evento por lead alterado; acompanhe por GET /leads/actions/{runId} e pelo evento `conversation:leads_bulk_update`. Um bloqueio com `businessPhoneId` confere o número antes (422 `lead_action_phone_unavailable`) e aplica o bloqueio no WhatsApp depois, sob limite de taxa. `export` confere o limite de 250.000 leads antes de congelar a seleção e responde 202 com o relatório (`report`, tipo `leads`), baixado por GET /reports/{id}/file só por quem pediu ou por quem tem as mesmas permissões. `meta_audience` congela a seleção (sem leads bloqueados), responde 202 com o público em montagem (`audience` com `id` e `status` `pending`) e envia à Meta em segundo plano; acompanhe por GET /leads/actions/audiences/{audienceId}. `call_list` (params `callList`: `name`, `assigneeIds`, `phoneSource` `identity` ou `contact` com `phoneLabel`) congela a seleção sob o id da lista (até 100.000 leads) e responde 202 com a lista em montagem (`callList` com `status` `building`); os itens são montados em segundo plano, pulando com o motivo os leads bloqueados, que pediram para não ser chamados, ou sem o número escolhido; acompanhe por GET /call-lists/{listId}. `send_template` e `send_unofficial` preparam o disparo ("Revisar"): congelam a seleção, montam as variáveis de cada lead, criam uma campanha parada por parte de até 150.000 leads com origem `lead_selection` e a chave do pedido (repetir o pedido devolve as mesmas campanhas), no departamento de quem pede, e respondem 202 com `send`, a revisão com as contagens por motivo e a cotação; nada é enviado até POST /leads/actions/sends/start, e POST /leads/actions/sends/cancel exclui o disparo. Sem o escopo de criação de quem pede responde 403 `send_creation_scope_missing`; numa conta com departamentos, sem `departmentId` responde 400 `send_department_required`; acima de 150.000 leads sem `split` responde 413 `send_selection_over_campaign_cap`, conferido antes de congelar a seleção. Uma recusa antes de criar qualquer campanha (contagem mudada, tamanho, parâmetros) libera a `Idempotency-Key` para o pedido corrigido; depois que uma parte existe, a chave fica presa ao pedido. Um segundo pedido com a mesma chave enquanto o primeiro ainda prepara responde 409 `send_preparing`: tente de novo em instantes. A leitura dos leads e a revisão passam pelo limite de consultas analíticas (503 com Retry-After quando ocupado). Permissões como em POST /leads/actions/preview.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			Idempotency-Key	header		string				true	"Chave única desta ação"
// @Param			body			body		LeadActionRequest	true	"Ação, parâmetros e seleção confirmada"
// @Success		202				{object}	ActionStartResponse
// @Failure		400				{object}	response.ErrorResponse
// @Failure		403				{object}	response.ErrorResponse
// @Failure		409				{object}	ActionSelectionChangedResponse
// @Failure		413				{object}	response.ErrorResponse
// @Failure		422				{object}	response.ErrorResponse
// @Failure		503				{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/actions [post]
func (h *LeadHandler) StartAction(w http.ResponseWriter, r *http.Request) {
	req, ok := h.actionRequest(w, r)
	if !ok {
		return
	}
	if !leadaction.ValidKey(req.IdempotencyKey) {
		writeActionError(w, leadaction.ErrIdempotencyKeyRequired)
		return
	}
	out, err := h.actions.Start(httpx.WithCreationScope(r, "").Context(), req)
	if err != nil {
		writeActionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusAccepted, ActionStartResponse{
		Action: string(req.Action), Run: runResponse(out.Run), Report: out.Report, Audience: audienceResponse(out.Audience), CallList: callListResponse(out), Send: out.Send,
	})
}

// @Summary		Andamento de uma ação em massa sobre leads
// @Description	Devolve a execução de uma ação de classificar, atribuir ou bloquear: status (`queued`, `running`, `done`, `failed`), fase (`edit` e, num bloqueio com número, `meta`) e contagens (`processed`, `changed`, `skipped` por motivo, `metaApplied`, `metaFailed`). Visível para quem pediu e para quem tem a permissão da ação; o valor de um campo sensível só aparece para quem tem `leads:read_sensitive` (senão `valueRedacted`).
// @Tags			Leads
// @Produce		json
// @Param			runId	path		string	true	"ID da execução"
// @Success		200		{object}	LeadActionRunResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/actions/{runId} [get]
func (h *LeadHandler) ActionRun(w http.ResponseWriter, r *http.Request) {
	if !h.actionsReady(w) {
		return
	}
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	run, err := h.actions.Run(r.Context(), a, mux.Vars(r)["runId"])
	if err != nil {
		writeActionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, runResponse(run))
}

// @Summary		Andamento de um público da Meta criado a partir de leads
// @Description	Devolve o público que POST /leads/actions (`meta_audience`) monta em segundo plano sobre a seleção congelada: `pending` enquanto lê os leads e envia à Meta, `done` com o público, `matched` e `skipped`, ou `failed` com `failureCode` (por exemplo `audience_terms_not_accepted`, `no_customers_matched`, `reconnect_required` ou `stalled` quando ficou parado mais de 30 minutos). Só quem pediu o lê; expira em 24 horas.
// @Tags			Leads
// @Produce		json
// @Param			audienceId	path		string	true	"ID do público em montagem"
// @Success		200			{object}	LeadAudienceResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/actions/audiences/{audienceId} [get]
func (h *LeadHandler) ActionAudience(w http.ResponseWriter, r *http.Request) {
	if !h.actionsReady(w) {
		return
	}
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	job, err := h.actions.Audience(r.Context(), a, mux.Vars(r)["audienceId"])
	if err != nil {
		writeActionError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, audienceResponse(job))
}

var actionStatuses = []struct {
	err    error
	status int
}{
	{leadaction.ErrForbidden, http.StatusForbidden},
	{leadaction.ErrIdempotencyKeyReused, http.StatusConflict},
	{leadaction.ErrInProgress, http.StatusConflict},
	{leadaction.ErrRunNotFound, http.StatusNotFound},
	{leadaction.ErrPreviewNotFound, http.StatusNotFound},
	{leadaction.ErrAudienceNotFound, http.StatusNotFound},
	{leadaction.ErrPhoneUnavailable, http.StatusUnprocessableEntity},
	{leadaction.ErrSnapshotLost, http.StatusConflict},
	{leadaction.ErrSelectionTooLarge, http.StatusRequestEntityTooLarge},
	{leadaction.ErrUnavailable, http.StatusServiceUnavailable},
	{leadaction.ErrRequirementUnknown, http.StatusInternalServerError},
}

func writeActionError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if httpx.WriteAnalyticsLimit(w, err, "Lead selection") {
		return
	}
	var changed *selection.CountChangedError
	if errors.As(err, &changed) {
		response.WriteSuccess(w, http.StatusConflict, ActionSelectionChangedResponse{
			Error: true, Code: selection.ErrorCode(err), Message: "A seleção mudou desde a contagem", Expected: changed.Expected, Matched: changed.Matched,
		})
		return
	}
	if code := leadaction.ErrorCode(err); code != "" {
		status := http.StatusBadRequest
		for _, known := range actionStatuses {
			if errors.Is(err, known.err) {
				status = known.status
				break
			}
		}
		response.WriteErrorWithCode(w, status, code, err.Error(), nil)
		return
	}
	if writeFilterRefusal(w, err) {
		return
	}
	if status, code, refused := otherActionRefusal(err); refused {
		response.WriteErrorWithCode(w, status, code, err.Error(), nil)
		return
	}
	response.WriteError(w, http.StatusInternalServerError, "Failed to run the lead action", nil)
}

func otherActionRefusal(err error) (int, string, bool) {
	if status, code, refused := sendRefusal(err); refused {
		return status, code, true
	}
	switch {
	case errors.Is(err, selection.ErrScopeDenied):
		return http.StatusForbidden, selection.ErrorCode(err), true
	case errors.Is(err, selection.ErrResolverUnavailable):
		return http.StatusServiceUnavailable, selection.ErrorCode(err), true
	case selection.ErrorCode(err) != "":
		return http.StatusBadRequest, selection.ErrorCode(err), true
	case errors.Is(err, customfield.ErrValueForbidden):
		return http.StatusForbidden, customfield.ErrorCode(err), true
	case customfield.ErrorCode(err) != "":
		return http.StatusBadRequest, customfield.ErrorCode(err), true
	case errors.Is(err, leaddomain.ErrLeadOwnerOutOfReach), errors.Is(err, leaddomain.ErrLeadForbidden):
		return http.StatusForbidden, leaddomain.ErrorCode(err), true
	case leaddomain.ErrorCode(err) != "":
		return http.StatusBadRequest, leaddomain.ErrorCode(err), true
	case errors.Is(err, report.ErrNotAllowed):
		return http.StatusForbidden, report.ErrorCode(err), true
	case errors.Is(err, report_usecase.ErrNotConfigured):
		return http.StatusServiceUnavailable, "reports_unavailable", true
	}
	if status, code, _, ok := advertisinghttp.Refusal(err); ok {
		return status, code, true
	}
	if status, code, ok := calllisthttp.Refusal(err); ok {
		return status, code, true
	}
	return 0, "", false
}

func callListResponse(out leadaction_usecase.Outcome) *calllisthttp.CallListResponse {
	if out.CallList == nil {
		return nil
	}
	list := calllisthttp.ViewResponseOf(*out.CallList)
	return &list
}
