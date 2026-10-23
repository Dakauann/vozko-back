package emailcampaignhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

type Actor = emailcampaign_usecase.Actor

type Campaigns interface {
	Create(ctx context.Context, a Actor, d emailcampaign.Draft) (*emailcampaign.Campaign, error)
	Get(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error)
	List(ctx context.Context, a Actor, q emailcampaign.ListQuery) ([]emailcampaign_usecase.ListItem, int64, error)
	Present(ctx context.Context, c emailcampaign.Campaign) (emailcampaign_usecase.CampaignView, error)
	Update(ctx context.Context, a Actor, campaignID string, d emailcampaign.Draft) (*emailcampaign.Campaign, error)
	Delete(ctx context.Context, a Actor, campaignID string) error
	TestSend(ctx context.Context, a Actor, campaignID string, to []string) ([]emailcampaign_usecase.TestResult, error)
	Review(ctx context.Context, a Actor, campaignID string) (emailcampaign_usecase.ReviewView, error)
	Launch(ctx context.Context, a Actor, campaignID string, at *time.Time) (*emailcampaign.Campaign, error)
	Unschedule(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error)
	Ready(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error)
	Pause(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error)
	Resume(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error)
	Stop(ctx context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error)
	Metrics(ctx context.Context, a Actor, campaignID string) (emailcampaign_usecase.CampaignMetrics, error)
	Timeline(ctx context.Context, a Actor, campaignID string) (emailcampaign_usecase.Timeline, error)
	Links(ctx context.Context, a Actor, campaignID string) ([]emailcampaign.LinkStat, error)
	Domains(ctx context.Context, a Actor, campaignID string) ([]emailcampaign.DomainStat, error)
	Recipients(ctx context.Context, a Actor, campaignID string, q emailcampaign_usecase.RecipientQuery) (emailcampaign_usecase.RecipientPage, error)
	Overview(ctx context.Context, a Actor, days int) (emailcampaign_usecase.Overview, error)
	CreateDesign(ctx context.Context, a Actor, in emailcampaign.DesignInput) (*emailcampaign.Design, error)
	GetDesign(ctx context.Context, a Actor, designID string) (*emailcampaign.Design, error)
	UpdateDesign(ctx context.Context, a Actor, designID string, in emailcampaign.DesignInput) (*emailcampaign.Design, error)
	DeleteDesign(ctx context.Context, a Actor, designID string) error
	ListDesigns(ctx context.Context, a Actor, kind emailcampaign.DesignKind, offset, limit int) ([]emailcampaign.Design, int64, error)
	Starters() []emaildesign.Starter
	Render(in emailcampaign_usecase.PreviewInput) (emailcampaign_usecase.Preview, error)
}

type Handler struct {
	campaigns Campaigns
}

func NewHandler(campaigns Campaigns) *Handler {
	return &Handler{campaigns: campaigns}
}

const (
	defaultPageSize = 50
	maxPageSize     = 200
	defaultDays     = 30
)

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (Actor, bool) {
	a, ok := httpx.WorkspaceActor(r)
	if !ok {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "workspace_required", "Workspace context is required", nil)
	}
	return a, ok
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "email_request_invalid", "Corpo da requisição inválido", nil)
		return false
	}
	return true
}

func paging(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	page = max(page, 1)
	if size <= 0 || size > maxPageSize {
		size = defaultPageSize
	}
	return page, size
}

func campaignID(r *http.Request) string { return mux.Vars(r)["campaignId"] }

func designID(r *http.Request) string { return mux.Vars(r)["designId"] }

func (h *Handler) writeCampaign(w http.ResponseWriter, r *http.Request, status int, c *emailcampaign.Campaign, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	view, err := h.campaigns.Present(r.Context(), *c)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, status, campaignResponse(view))
}

// @Summary		Listar campanhas de e-mail
// @Description	Campanhas do workspace, das mais novas para as mais antigas, com filtro por status e busca pelo nome. Cada item traz as métricas de envio (`metrics`: enviados, entregues e taxas de entrega, abertura e clique em pontos-base, como no detalhe) e os comandos que o status permite (`actions`). Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			status		query		string	false	"Status"	Enums(draft,ready,scheduled,running,paused,held,completed,stopped)
// @Param			search		query		string	false	"Parte do nome"
// @Param			page		query		int		false	"Página (1 em diante)"
// @Param			page_size	query		int		false	"Itens por página (até 200)"
// @Success		200			{object}	emailcampaignhttp.CampaignListResponse
// @Failure		400			{object}	response.ErrorResponse	"email_campaign_status_invalid"
// @Security		BearerAuth
// @Router			/email/campaigns [get]
func (h *Handler) ListCampaigns(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	page, size := paging(r)
	q := emailcampaign.ListQuery{Status: emailcampaign.Status(r.URL.Query().Get("status")), Search: r.URL.Query().Get("search"), Offset: (page - 1) * size, Limit: size}
	items, total, err := h.campaigns.List(r.Context(), a, q)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, campaignList(items, total, page, size))
}

// @Summary		Criar campanha de e-mail
// @Description	Cria um rascunho. Só o nome é obrigatório; documento, assunto, remetente e público podem vir depois e são conferidos na revisão. Exige `email_marketing:create`.
// @Tags			E-mail marketing
// @Accept			json
// @Produce		json
// @Param			body	body		emailcampaignhttp.CampaignRequest	true	"Rascunho"
// @Success		201		{object}	emailcampaignhttp.CampaignResponse
// @Failure		400		{object}	response.ErrorResponse	"email_campaign_name_required, email_design_malformed, email_audience_invalid, email_ab_test_percent_invalid"
// @Security		BearerAuth
// @Router			/email/campaigns [post]
func (h *Handler) CreateCampaign(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CampaignRequest
	if !decode(w, r, &req) {
		return
	}
	d, err := req.draft()
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := h.campaigns.Create(r.Context(), a, d)
	h.writeCampaign(w, r, http.StatusCreated, c, err)
}

// @Summary		Detalhe da campanha de e-mail
// @Description	A campanha com documento, público, teste A/B e cobrança. `actions` lista os comandos que o status permite; `hold` explica uma espera (retomada prevista no limite diário e o motivo da pausa da conta); `billing` traz o cobrado, o devolvido no fechamento, o valor final e, durante o envio, a estimativa a devolver (`refund_pending_micros`), com os valores em reais pela cotação atual ou nulos se a cotação não puder ser lida; `time_zone` é o fuso dos horários de agendamento. Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaignhttp.CampaignResponse
// @Failure		404			{object}	response.ErrorResponse	"email_campaign_not_found"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId} [get]
func (h *Handler) GetCampaign(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	c, err := h.campaigns.Get(r.Context(), a, campaignID(r))
	h.writeCampaign(w, r, http.StatusOK, c, err)
}

// @Summary		Editar campanha de e-mail
// @Description	Substitui o conteúdo do rascunho ou da campanha agendada. Campanhas em envio ou encerradas não mudam. Exige `email_marketing:update`.
// @Tags			E-mail marketing
// @Accept			json
// @Produce		json
// @Param			campaignId	path		string								true	"Campanha"
// @Param			body		body		emailcampaignhttp.CampaignRequest	true	"Conteúdo completo"
// @Success		200			{object}	emailcampaignhttp.CampaignResponse
// @Failure		409			{object}	response.ErrorResponse	"email_campaign_not_editable"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId} [patch]
func (h *Handler) UpdateCampaign(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req CampaignRequest
	if !decode(w, r, &req) {
		return
	}
	d, err := req.draft()
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := h.campaigns.Update(r.Context(), a, campaignID(r), d)
	h.writeCampaign(w, r, http.StatusOK, c, err)
}

// @Summary		Excluir rascunho de campanha
// @Description	Exclui um rascunho. Campanhas agendadas, em envio ou encerradas não são excluídas. Exige `email_marketing:delete`.
// @Tags			E-mail marketing
// @Param			campaignId	path	string	true	"Campanha"
// @Success		204
// @Failure		409	{object}	response.ErrorResponse	"email_campaign_not_editable"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId} [delete]
func (h *Handler) DeleteCampaign(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.campaigns.Delete(r.Context(), a, campaignID(r)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Enviar teste da campanha
// @Description	Envia o e-mail como ficará para até 5 endereços. Cada endereço é cobrado do saldo pelo preço normal de um e-mail antes do envio; o que não sair (suprimido, limite diário, recusado ou falha) é devolvido. Sem saldo para todos os testes, nada é enviado. A resposta traz o resultado de cada endereço. Exige `email_marketing:update`.
// @Tags			E-mail marketing
// @Accept			json
// @Produce		json
// @Param			campaignId	path		string							true	"Campanha"
// @Param			body		body		emailcampaignhttp.TestSendRequest	true	"Destinatários"
// @Success		200			{object}	emailcampaignhttp.TestSendResponse
// @Failure		400			{object}	response.ErrorResponse	"email_test_recipients_invalid, email_test_address_invalid"
// @Failure		422			{object}	response.ErrorResponse	"email_design_invalid, email_subject_invalid"
// @Failure		402			{object}	response.ErrorResponse	"email_balance_insufficient (details.missing_micros: o que falta para cobrir todos os testes)"
// @Failure		503			{object}	response.ErrorResponse	"email_account_reactivation_failed"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/test [post]
func (h *Handler) TestSend(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req TestSendRequest
	if !decode(w, r, &req) {
		return
	}
	results, err := h.campaigns.TestSend(r.Context(), a, campaignID(r), req.To)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, TestSendResponse{Results: results})
}

// @Summary		Revisar campanha antes do disparo
// @Description	Resolve o público e separa quem não recebe e por quê (sem e-mail, endereço inválido, repetido, descadastrado, com opt-out geral, bloqueado ou suprimido; quem não tem consentimento registrado recebe, porque o consentimento é responsabilidade do cliente), orça o envio em micros de dólar e em reais pela cotação atual, mostra o saldo, o fuso dos agendamentos e a verificação de disparo (impedimentos e dicas). Sem cotação a revisão é recusada. Exige `email_marketing:update`.
// @Description	A higiene da lista é gratuita e também separa, em `audience.skipped`: `bad_address` (voltou como inexistente em algum envio da plataforma), `no_mail_domain` (o domínio não recebe e-mails), `disposable` (e-mail temporário), `domain_unknown` (o domínio não respondeu à consulta), `inactive` (sem interação há mais de 6 meses depois de 5 campanhas) e `domain_checking` (o domínio ainda está sendo verificado; o disparo espera com o impedimento `domains_checking`, que some sozinho em instantes, e um agendamento espera um minuto e tenta de novo). `audience.never_mailed` conta quem nunca recebeu e-mail deste workspace; quando são 20% ou mais de mais de 200 destinatários, `test_batch` diz o tamanho do lote de teste (10% do público, de 200 a 1.000): só ele sai primeiro e o resto espera os resultados (mais de 3% de endereços inexistentes ou mais de 0,1% de denúncias de spam param a campanha com `hold_reason` `test_batch_bounces` ou `test_batch_complaints`, e ela só pode ser encerrada).
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaignhttp.ReviewResponse
// @Failure		404			{object}	response.ErrorResponse	"email_campaign_not_found"
// @Failure		503			{object}	response.ErrorResponse	"email_exchange_rate_unavailable"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/review [get]
func (h *Handler) Review(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	review, err := h.campaigns.Review(r.Context(), a, campaignID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, reviewResponse(review))
}

// @Summary		Disparar ou agendar campanha
// @Description	Sem `scheduled_at`, refaz a revisão, materializa os destinatários, reativa a conta de envio se estiver inativa, cobra o envio do saldo e começa a enviar. Com `scheduled_at` no futuro, agenda; a cobrança acontece no início. Uma campanha pronta (`ready`, comando `start`) inicia por aqui, sem `scheduled_at`, refazendo a revisão e a verificação inteiras como no disparo de um rascunho. Impedimentos voltam em `details.issues`. Exige `email_marketing:start`.
// @Tags			E-mail marketing
// @Accept			json
// @Produce		json
// @Param			campaignId	path		string							true	"Campanha"
// @Param			body		body		emailcampaignhttp.LaunchRequest	false	"Agendamento"
// @Success		200			{object}	emailcampaignhttp.CampaignResponse
// @Failure		402			{object}	response.ErrorResponse	"email_balance_insufficient (details.missing_micros)"
// @Failure		409			{object}	response.ErrorResponse	"email_campaign_already_running, email_campaign_not_draft"
// @Failure		422			{object}	response.ErrorResponse	"email_campaign_blocked (details.issues)"
// @Failure		503			{object}	response.ErrorResponse	"email_account_reactivation_failed (a conta inativa não voltou; nada foi cobrado)"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/launch [post]
func (h *Handler) Launch(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req LaunchRequest
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	c, err := h.campaigns.Launch(r.Context(), a, campaignID(r), req.ScheduledAt)
	h.writeCampaign(w, r, http.StatusOK, c, err)
}

// @Summary		Deixar campanha pronta
// @Description	Refaz a revisão e a verificação de disparo do rascunho, como no disparo, e deixa a campanha pronta (`ready`) para iniciar quando quiser, sem materializar destinatários nem mexer no saldo. Uma campanha pronta volta a rascunho quando é editada, pode ser agendada ou excluída, e inicia por POST /email/campaigns/{campaignId}/launch, que refaz a verificação inteira antes de enviar. Exige `email_marketing:start`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaignhttp.CampaignResponse
// @Failure		409			{object}	response.ErrorResponse	"email_campaign_not_draft"
// @Failure		422			{object}	response.ErrorResponse	"email_campaign_blocked (details.issues)"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/ready [post]
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	c, err := h.campaigns.Ready(r.Context(), a, campaignID(r))
	h.writeCampaign(w, r, http.StatusOK, c, err)
}

// @Summary		Cancelar agendamento
// @Description	Devolve a campanha agendada para rascunho. Exige `email_marketing:start`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaignhttp.CampaignResponse
// @Failure		409			{object}	response.ErrorResponse	"email_campaign_not_scheduled"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/unschedule [post]
func (h *Handler) Unschedule(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	c, err := h.campaigns.Unschedule(r.Context(), a, campaignID(r))
	h.writeCampaign(w, r, http.StatusOK, c, err)
}

// @Summary		Pausar campanha
// @Description	Pausa o envio; os destinatários pendentes continuam reservados. Exige `email_marketing:stop`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaignhttp.CampaignResponse
// @Failure		409			{object}	response.ErrorResponse	"email_campaign_not_running, email_campaign_test_batch_failed"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/pause [post]
func (h *Handler) Pause(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	c, err := h.campaigns.Pause(r.Context(), a, campaignID(r))
	h.writeCampaign(w, r, http.StatusOK, c, err)
}

// @Summary		Retomar campanha
// @Description	Retoma uma campanha pausada ou em espera. Exige `email_marketing:start`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaignhttp.CampaignResponse
// @Failure		409			{object}	response.ErrorResponse	"email_campaign_already_running, email_campaign_finished, email_campaign_not_launched, email_campaign_test_batch_failed"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/resume [post]
func (h *Handler) Resume(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	c, err := h.campaigns.Resume(r.Context(), a, campaignID(r))
	h.writeCampaign(w, r, http.StatusOK, c, err)
}

// @Summary		Parar campanha
// @Description	Encerra o envio de vez e devolve ao saldo o valor dos e-mails não enviados. Exige `email_marketing:stop`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaignhttp.CampaignResponse
// @Failure		409			{object}	response.ErrorResponse	"email_campaign_finished, email_campaign_not_launched"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/stop [post]
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	c, err := h.campaigns.Stop(r.Context(), a, campaignID(r))
	h.writeCampaign(w, r, http.StatusOK, c, err)
}

// @Summary		Métricas da campanha
// @Description	Enviados, entregues, aberturas e cliques únicos de pessoas (aberturas de robôs ficam fora), rejeições, reclamações, descadastros, ignorados por motivo, taxas em pontos-base e o resultado do teste A/B. Atualiza a cada 15 s durante o envio. Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaign_usecase.CampaignMetrics
// @Failure		404			{object}	response.ErrorResponse	"email_campaign_not_found"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/metrics [get]
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	m, err := h.campaigns.Metrics(r.Context(), a, campaignID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, m)
}

// @Summary		Linha do tempo da campanha
// @Description	Envios, entregas, primeiras aberturas, primeiros cliques e rejeições por hora (campanhas de até 48 h) ou por dia, com os baldes no fuso informado em `time_zone`. `refresh_after_seconds` diz quando consultar de novo (0 quando a campanha terminou). Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaign_usecase.Timeline
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/timeline [get]
func (h *Handler) Timeline(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	t, err := h.campaigns.Timeline(r.Context(), a, campaignID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, t)
}

// @Summary		Links clicados da campanha
// @Description	Os 50 links mais clicados, com cliques e pessoas únicas; cliques de robôs ficam fora. Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaignhttp.LinkListResponse
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/links [get]
func (h *Handler) Links(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	links, err := h.campaigns.Links(r.Context(), a, campaignID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	if links == nil {
		links = []emailcampaign.LinkStat{}
	}
	response.WriteSuccess(w, http.StatusOK, LinkListResponse{Items: links})
}

// @Summary		Provedores de caixa postal da campanha
// @Description	Resultado por provedor (gmail, microsoft, yahoo, apple, uol, other). Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Success		200			{object}	emailcampaignhttp.DomainListResponse
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/domains [get]
func (h *Handler) Domains(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	domains, err := h.campaigns.Domains(r.Context(), a, campaignID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	if domains == nil {
		domains = []emailcampaign.DomainStat{}
	}
	response.WriteSuccess(w, http.StatusOK, DomainListResponse{Items: domains})
}

// @Summary		Destinatários da campanha
// @Description	Destinatários paginados com o nome do lead (`lead_name`) e o estado da mensagem; filtra por status, motivo de exclusão, variante e endereço. Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			campaignId	path		string	true	"Campanha"
// @Param			status		query		string	false	"Status"	Enums(pending,skipped,sent,failed)
// @Param			skip_reason	query		string	false	"Motivo"	Enums(no_email,invalid_address,duplicate,opted_out,blocked,unsubscribed,suppressed)
// @Param			variant		query		string	false	"Variante"	Enums(a,b,winner)
// @Param			search		query		string	false	"Parte do endereço"
// @Param			page		query		int		false	"Página"
// @Param			page_size	query		int		false	"Itens por página"
// @Success		200			{object}	emailcampaignhttp.RecipientListResponse
// @Failure		400			{object}	response.ErrorResponse	"email_recipient_filter_invalid"
// @Security		BearerAuth
// @Router			/email/campaigns/{campaignId}/recipients [get]
func (h *Handler) Recipients(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	page, size := paging(r)
	q := r.URL.Query()
	p, err := h.campaigns.Recipients(r.Context(), a, campaignID(r), emailcampaign_usecase.RecipientQuery{
		Filter: emailcampaign.RecipientFilter{
			Status: emailcampaign.RecipientStatus(q.Get("status")), SkipReason: emailcampaign.SkipReason(q.Get("skip_reason")),
			Variant: emailcampaign.Variant(q.Get("variant")), Search: q.Get("search"),
		},
		Page: page, PageSize: size,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, recipientList(p))
}

// @Summary		Visão geral do e-mail
// @Description	Totais e taxas da janela (campanhas e envios avulsos), série diária por dia no fuso de `time_zone`, campanhas com mais envios e reputação em pontos-base contra as faixas de atenção e de limite: rejeição de 2% e 5%, reclamação de 0,1% e 0,3% (`*_level`: ok, warning ou over). Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			days	query		int	false	"Janela em dias (1 a 90, padrão 30)"
// @Success		200		{object}	emailcampaign_usecase.Overview
// @Failure		400		{object}	response.ErrorResponse	"email_overview_days_invalid"
// @Security		BearerAuth
// @Router			/email/overview [get]
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	days := defaultDays
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, emailcampaign_usecase.ErrDaysInvalid)
			return
		}
		days = n
	}
	o, err := h.campaigns.Overview(r.Context(), a, days)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, o)
}

// @Summary		Listar modelos de e-mail
// @Description	Modelos salvos do workspace. Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			kind		query		string	false	"Tipo"	Enums(template,campaign)
// @Param			page		query		int		false	"Página"
// @Param			page_size	query		int		false	"Itens por página"
// @Success		200			{object}	emailcampaignhttp.DesignListResponse
// @Security		BearerAuth
// @Router			/email/designs [get]
func (h *Handler) ListDesigns(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	page, size := paging(r)
	items, total, err := h.campaigns.ListDesigns(r.Context(), a, emailcampaign.DesignKind(r.URL.Query().Get("kind")), (page-1)*size, size)
	if err != nil {
		writeError(w, err)
		return
	}
	out := DesignListResponse{Items: make([]DesignResponse, 0, len(items)), Total: total, Page: page, PageSize: size}
	for _, d := range items {
		out.Items = append(out.Items, designResponse(d))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Criar modelo de e-mail
// @Description	Salva um documento válido do editor de blocos. Exige `email_marketing:create`.
// @Tags			E-mail marketing
// @Accept			json
// @Produce		json
// @Param			body	body		emailcampaignhttp.DesignRequest	true	"Modelo"
// @Success		201		{object}	emailcampaignhttp.DesignResponse
// @Failure		422		{object}	response.ErrorResponse	"email_design_invalid (details.issues)"
// @Security		BearerAuth
// @Router			/email/designs [post]
func (h *Handler) CreateDesign(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req DesignRequest
	if !decode(w, r, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		writeError(w, err)
		return
	}
	d, err := h.campaigns.CreateDesign(r.Context(), a, in)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, designResponse(*d))
}

// @Summary		Modelos prontos
// @Description	Galeria de documentos iniciais (em branco, newsletter, promoção, anúncio, evento). Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Success		200	{object}	emailcampaignhttp.StarterListResponse
// @Security		BearerAuth
// @Router			/email/designs/starters [get]
func (h *Handler) Starters(w http.ResponseWriter, r *http.Request) {
	response.WriteSuccess(w, http.StatusOK, StarterListResponse{Items: h.campaigns.Starters()})
}

// @Summary		Detalhe do modelo de e-mail
// @Description	Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Produce		json
// @Param			designId	path		string	true	"Modelo"
// @Success		200			{object}	emailcampaignhttp.DesignResponse
// @Failure		404			{object}	response.ErrorResponse	"email_design_not_found"
// @Security		BearerAuth
// @Router			/email/designs/{designId} [get]
func (h *Handler) GetDesign(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	d, err := h.campaigns.GetDesign(r.Context(), a, designID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, designResponse(*d))
}

// @Summary		Editar modelo de e-mail
// @Description	Exige `email_marketing:update`.
// @Tags			E-mail marketing
// @Accept			json
// @Produce		json
// @Param			designId	path		string							true	"Modelo"
// @Param			body		body		emailcampaignhttp.DesignRequest	true	"Modelo"
// @Success		200			{object}	emailcampaignhttp.DesignResponse
// @Failure		422			{object}	response.ErrorResponse	"email_design_invalid (details.issues)"
// @Security		BearerAuth
// @Router			/email/designs/{designId} [put]
func (h *Handler) UpdateDesign(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var req DesignRequest
	if !decode(w, r, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		writeError(w, err)
		return
	}
	d, err := h.campaigns.UpdateDesign(r.Context(), a, designID(r), in)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, designResponse(*d))
}

// @Summary		Excluir modelo de e-mail
// @Description	Exige `email_marketing:delete`.
// @Tags			E-mail marketing
// @Param			designId	path	string	true	"Modelo"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse	"email_design_not_found"
// @Security		BearerAuth
// @Router			/email/designs/{designId} [delete]
func (h *Handler) DeleteDesign(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	if err := h.campaigns.DeleteDesign(r.Context(), a, designID(r)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Pré-visualizar e-mail
// @Description	Renderiza o documento no servidor com dados de exemplo (ou os enviados em `sample`) e devolve HTML, texto, o assunto resolvido e as dicas de qualidade. O navegador nunca monta o e-mail final. Exige `email_marketing:read`.
// @Tags			E-mail marketing
// @Accept			json
// @Produce		json
// @Param			body	body		emailcampaignhttp.RenderRequest	true	"Documento"
// @Success		200		{object}	emailcampaign_usecase.Preview
// @Failure		422		{object}	response.ErrorResponse	"email_design_invalid, email_subject_invalid (details.issues)"
// @Security		BearerAuth
// @Router			/email/render [post]
func (h *Handler) Render(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.actor(w, r); !ok {
		return
	}
	var req RenderRequest
	if !decode(w, r, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		writeError(w, err)
		return
	}
	preview, err := h.campaigns.Render(in)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, preview)
}
