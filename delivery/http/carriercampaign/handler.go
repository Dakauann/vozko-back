package carriercampaign

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	cm "vozko/domain/carriermessaging"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	ccuc "vozko/usecases/carriercampaign"
)

const (
	defaultPageSize     = 20
	defaultOverviewDays = 7
	allChannels         = "all"
)

type API interface {
	ListSummaries(ctx context.Context, workspaceID string, in cc.ListCampaignsInput) (*shared.PaginatedResult[ccuc.CampaignSummary], error)
	Create(ctx context.Context, workspaceID string, a ccuc.Actor, d ccuc.Draft) (*cc.Campaign, error)
	Get(ctx context.Context, workspaceID, id string) (*cc.Campaign, error)
	Update(ctx context.Context, workspaceID, id string, d ccuc.Draft) (*cc.Campaign, error)
	Delete(ctx context.Context, workspaceID, id string) error
	Duplicate(ctx context.Context, workspaceID, id string, a ccuc.Actor, name string) (*cc.Campaign, error)
	Estimate(ctx context.Context, workspaceID, id string, a ccuc.Actor) (ccuc.EstimateResult, error)
	TestSend(ctx context.Context, workspaceID, id string, a ccuc.Actor, phone string, variant int) (*cm.Message, error)
	Launch(ctx context.Context, workspaceID, id string, a ccuc.Actor) (ccuc.LaunchResult, error)
	Ready(ctx context.Context, workspaceID, id string, a ccuc.Actor) (*cc.Campaign, error)
	Pause(ctx context.Context, workspaceID, id string) error
	Resume(ctx context.Context, workspaceID, id string) error
	Stop(ctx context.Context, workspaceID, id string) error
	Funnel(ctx context.Context, workspaceID, id string) (ccuc.FunnelView, error)
	RunFunnel(ctx context.Context, workspaceID, id, runID string) (ccuc.FunnelView, error)
	Variants(ctx context.Context, workspaceID, id string) ([]cc.VariantStat, error)
	Buttons(ctx context.Context, workspaceID, id string, variant int) ([]cc.ButtonStat, error)
	Carriers(ctx context.Context, workspaceID, id string) ([]cc.CarrierStat, error)
	Timeline(ctx context.Context, workspaceID, id string, bucket campaign.TimelineBucket) (*campaign.Timeline, error)
	Recipients(ctx context.Context, workspaceID, id string, q ccuc.RecipientQuery) (*shared.PaginatedResult[ccuc.RecipientRow], error)
	Overview(ctx context.Context, workspaceID string, days int, channel string) (ccuc.OverviewResult, error)
}

type Handler struct {
	api API
}

func NewHandler(api API) *Handler {
	return &Handler{api: api}
}

func actorOf(w http.ResponseWriter, r *http.Request) (string, ccuc.Actor, bool) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return "", ccuc.Actor{}, false
	}
	viewer, ok := httpx.LeadActor(r)
	if !ok {
		writeError(w, ccuc.ErrActorRequired, "actor")
		return "", ccuc.Actor{}, false
	}
	return ws, ccuc.Actor{UserID: viewer.UserID, IsAdmin: viewer.IsAdmin, DepartmentID: middleware.SelectedDepartmentID(r)}, true
}

func intParam(values url.Values, key string, fallback int) (int, bool) {
	raw := strings.TrimSpace(values.Get(key))
	if raw == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(raw)
	return n, err == nil
}

func pageOf(values url.Values) (shared.Pagination, bool) {
	page, okPage := intParam(values, "page", 1)
	limit, okLimit := intParam(values, "limit", defaultPageSize)
	if !okPage || !okLimit || page < 1 || limit < 1 || limit > shared.MaxPageSize {
		return shared.Pagination{}, false
	}
	return shared.Pagination{Page: page, PageSize: limit}, true
}

func (h *Handler) campaign(w http.ResponseWriter, c *cc.Campaign, status int) {
	response.WriteSuccess(w, status, campaignDTO(c))
}

// @Summary		Campanhas RCS e SMS
// @Description	Lista as campanhas do workspace com os números principais de cada uma.
// @Tags			RCS e SMS
// @Produce		json
// @Param			channel	query		string	false	"rcs ou sms"
// @Param			status	query		string	false	"draft, scheduled, running, paused, completed ou stopped"
// @Param			page	query		int		false	"Página"
// @Param			limit	query		int		false	"Itens por página, até 100"
// @Success		200		{object}	CampaignListResponse
// @Router			/carrier/campaigns [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, ok := pageOf(q)
	if !ok {
		writeError(w, errQueryInvalid, "list campaigns")
		return
	}
	in := cc.ListCampaignsInput{
		Channel: cm.Channel(strings.TrimSpace(q.Get("channel"))), Status: cc.Status(strings.TrimSpace(q.Get("status"))),
		Search: strings.TrimSpace(q.Get("search")), Pagination: page,
	}
	result, err := h.api.ListSummaries(r.Context(), ws, in)
	if err != nil {
		writeError(w, err, "list campaigns")
		return
	}
	out := CampaignListResponse{Items: make([]CampaignSummaryDTO, 0, len(result.Items)), Total: result.TotalItems}
	for _, s := range result.Items {
		out.Items = append(out.Items, summaryDTO(s))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Criar campanha RCS ou SMS
// @Description	Cria a campanha como rascunho. Nada é enviado até iniciar.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			body	body		CampaignRequest	true	"Campanha"
// @Success		201		{object}	CampaignDTO
// @Router			/carrier/campaigns [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	ws, a, ok := actorOf(w, r)
	if !ok {
		return
	}
	var body CampaignRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	draft, err := body.draft()
	if err != nil {
		writeError(w, err, "create campaign")
		return
	}
	c, err := h.api.Create(r.Context(), ws, a, draft)
	if err != nil {
		writeError(w, err, "create campaign")
		return
	}
	h.campaign(w, c, http.StatusCreated)
}

// @Summary		Campanha RCS ou SMS
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{object}	CampaignDTO
// @Router			/carrier/campaigns/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	c, err := h.api.Get(r.Context(), ws, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "get campaign")
		return
	}
	h.campaign(w, c, http.StatusOK)
}

// @Summary		Editar campanha RCS ou SMS
// @Description	Substitui o rascunho. Uma campanha agendada volta a ser rascunho e precisa ser iniciada de novo.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"Campanha"
// @Param			body	body		CampaignRequest	true	"Campanha"
// @Success		200		{object}	CampaignDTO
// @Router			/carrier/campaigns/{id} [patch]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	var body CampaignRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	draft, err := body.draft()
	if err != nil {
		writeError(w, err, "update campaign")
		return
	}
	c, err := h.api.Update(r.Context(), ws, mux.Vars(r)["id"], draft)
	if err != nil {
		writeError(w, err, "update campaign")
		return
	}
	h.campaign(w, c, http.StatusOK)
}

// @Summary		Excluir rascunho de campanha
// @Tags			RCS e SMS
// @Param			id	path	string	true	"Campanha"
// @Success		204
// @Router			/carrier/campaigns/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	if err := h.api.Delete(r.Context(), ws, mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "delete campaign")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary		Duplicar campanha
// @Description	Copia a campanha como um novo rascunho sem agendamento.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"Campanha"
// @Param			body	body		DuplicateRequest	true	"Nome da cópia"
// @Success		201		{object}	CampaignDTO
// @Router			/carrier/campaigns/{id}/duplicate [post]
func (h *Handler) Duplicate(w http.ResponseWriter, r *http.Request) {
	ws, a, ok := actorOf(w, r)
	if !ok {
		return
	}
	var body DuplicateRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	c, err := h.api.Duplicate(r.Context(), ws, mux.Vars(r)["id"], a, body.Name)
	if err != nil {
		writeError(w, err, "duplicate campaign")
		return
	}
	h.campaign(w, c, http.StatusCreated)
}

// @Summary		Estimativa de custo da campanha
// @Description	Conta o público, calcula o pior caso de custo, o que falta no saldo com a reserva para respostas e se o remetente está pronto. `recipients` é o número de leads selecionados (é sobre ele que o custo é calculado e a seleção é confirmada) e `count` é a contagem do passo de público com as regras do RCS e SMS (sem telefone, bloqueados, com opt-out, suprimidos e repetidos em `out`, e `sendable`), a mesma de `POST /campaign-audience/carrier/count`.
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{object}	EstimateDTO
// @Router			/carrier/campaigns/{id}/estimate [post]
func (h *Handler) Estimate(w http.ResponseWriter, r *http.Request) {
	ws, a, ok := actorOf(w, r)
	if !ok {
		return
	}
	e, err := h.api.Estimate(r.Context(), ws, mux.Vars(r)["id"], a)
	if err != nil {
		writeError(w, err, "estimate campaign")
		return
	}
	response.WriteSuccess(w, http.StatusOK, estimateDTO(e))
}

// @Summary		Envio de teste da campanha
// @Description	Envia uma variação para um número, preenchendo as variáveis com o lead desse número. Conta nos envios de teste gratuitos do dia.
// @Tags			RCS e SMS
// @Accept			json
// @Produce		json
// @Param			id		path		string			true	"Campanha"
// @Param			body	body		TestSendRequest	true	"Número e variação"
// @Success		200		{object}	TestSendDTO
// @Router			/carrier/campaigns/{id}/test [post]
func (h *Handler) TestSend(w http.ResponseWriter, r *http.Request) {
	ws, a, ok := actorOf(w, r)
	if !ok {
		return
	}
	var body TestSendRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	m, err := h.api.TestSend(r.Context(), ws, mux.Vars(r)["id"], a, body.Phone, body.Variant)
	if err != nil {
		writeError(w, err, "test send")
		return
	}
	response.WriteSuccess(w, http.StatusOK, testSendDTO(m))
}

// @Summary		Iniciar campanha
// @Description	Com agendamento ou repetição, agenda. Sem eles, congela o público e começa a enviar agora.
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{object}	CampaignDTO
// @Router			/carrier/campaigns/{id}/launch [post]
func (h *Handler) Launch(w http.ResponseWriter, r *http.Request) {
	ws, a, ok := actorOf(w, r)
	if !ok {
		return
	}
	result, err := h.api.Launch(r.Context(), ws, mux.Vars(r)["id"], a)
	if err != nil {
		writeError(w, err, "launch campaign")
		return
	}
	h.campaign(w, result.Campaign, http.StatusOK)
}

// @Summary		Deixar campanha pronta
// @Description	Confere tudo o que o início confere (canal, mensagem, remetente, público confirmado, saldo) e deixa o rascunho pronto para começar quando a pessoa quiser, por POST /carrier/campaigns/{id}/launch. Nada é congelado, enfileirado ou cobrado. Só um rascunho sem agendamento nem repetição fica pronto: 409 `carrier_campaign_not_draft` ou `carrier_campaign_ready_scheduled`. Editar uma campanha pronta a devolve a rascunho; ela pode ser agendada, iniciada ou excluída, e parar responde 409 `carrier_campaign_not_launched`.
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{object}	CampaignDTO
// @Failure		402	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Failure		422	{object}	response.ErrorResponse
// @Router			/carrier/campaigns/{id}/ready [post]
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ws, a, ok := actorOf(w, r)
	if !ok {
		return
	}
	c, err := h.api.Ready(r.Context(), ws, mux.Vars(r)["id"], a)
	if err != nil {
		writeError(w, err, "ready campaign")
		return
	}
	h.campaign(w, c, http.StatusOK)
}

func (h *Handler) lifecycle(action func(context.Context, string, string) error, subject string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, ok := httpx.RequireWorkspace(w, r)
		if !ok {
			return
		}
		id := mux.Vars(r)["id"]
		if err := action(r.Context(), ws, id); err != nil {
			writeError(w, err, subject)
			return
		}
		c, err := h.api.Get(r.Context(), ws, id)
		if err != nil {
			writeError(w, err, subject)
			return
		}
		h.campaign(w, c, http.StatusOK)
	}
}

// @Summary		Pausar campanha
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{object}	CampaignDTO
// @Router			/carrier/campaigns/{id}/pause [post]
func (h *Handler) Pause(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(h.api.Pause, "pause campaign")(w, r)
}

// @Summary		Retomar campanha
// @Description	Confere o remetente antes de voltar a enviar.
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{object}	CampaignDTO
// @Router			/carrier/campaigns/{id}/resume [post]
func (h *Handler) Resume(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(h.api.Resume, "resume campaign")(w, r)
}

// @Summary		Encerrar campanha
// @Description	Para de enviar de vez. Uma campanha agendada perde o agendamento.
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{object}	CampaignDTO
// @Router			/carrier/campaigns/{id}/stop [post]
func (h *Handler) Stop(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(h.api.Stop, "stop campaign")(w, r)
}

// @Summary		Funil da campanha
// @Description	Contagens do envio até a conversão e as taxas de 0 a 1, nulas quando não há base para calcular.
// @Tags			RCS e SMS
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Param			run_id	query		string	false	"Execução de uma campanha recorrente"
// @Success		200		{object}	FunnelDTO
// @Router			/carrier/campaigns/{id}/funnel [get]
func (h *Handler) Funnel(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]
	var view ccuc.FunnelView
	var err error
	if runID := strings.TrimSpace(r.URL.Query().Get("run_id")); runID != "" {
		view, err = h.api.RunFunnel(r.Context(), ws, id, runID)
	} else {
		view, err = h.api.Funnel(r.Context(), ws, id)
	}
	if err != nil {
		writeError(w, err, "campaign funnel")
		return
	}
	response.WriteSuccess(w, http.StatusOK, funnelDTO(view))
}

// @Summary		Resultado por variação
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{array}		VariantStatDTO
// @Router			/carrier/campaigns/{id}/variants [get]
func (h *Handler) Variants(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	stats, err := h.api.Variants(r.Context(), ws, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "campaign variants")
		return
	}
	out := make([]VariantStatDTO, 0, len(stats))
	for _, s := range stats {
		out = append(out, VariantStatDTO{Variant: s.Index, Name: s.Name, Funnel: countsDTO(s.Funnel), Rates: ratesDTO(s.Rates)})
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Toques e cliques por botão
// @Description	Botões de uma variação RCS. Campanhas de SMS não têm botões.
// @Tags			RCS e SMS
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Param			variant	query		int		false	"Variação, a primeira é 0"
// @Success		200		{array}		ButtonStatDTO
// @Router			/carrier/campaigns/{id}/buttons [get]
func (h *Handler) Buttons(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	variant, ok := intParam(r.URL.Query(), "variant", 0)
	if !ok {
		writeError(w, errQueryInvalid, "campaign buttons")
		return
	}
	stats, err := h.api.Buttons(r.Context(), ws, mux.Vars(r)["id"], variant)
	if err != nil {
		writeError(w, err, "campaign buttons")
		return
	}
	out := make([]ButtonStatDTO, 0, len(stats))
	for _, b := range stats {
		out = append(out, ButtonStatDTO{Card: b.Key.Card, Index: b.Key.Index, Label: b.Label, Kind: string(b.Kind), Taps: b.Taps, Clicks: b.Clicks})
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Entrega por operadora
// @Tags			RCS e SMS
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{array}		CarrierStatDTO
// @Router			/carrier/campaigns/{id}/carriers [get]
func (h *Handler) Carriers(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	stats, err := h.api.Carriers(r.Context(), ws, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "campaign carriers")
		return
	}
	response.WriteSuccess(w, http.StatusOK, carrierDTOs(stats))
}

// @Summary		Linha do tempo da campanha
// @Tags			RCS e SMS
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Param			bucket	query		string	false	"minute, hour ou day"
// @Success		200		{object}	TimelineDTO
// @Router			/carrier/campaigns/{id}/timeline [get]
func (h *Handler) Timeline(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	bucket := campaign.TimelineBucket(strings.TrimSpace(r.URL.Query().Get("bucket")))
	timeline, err := h.api.Timeline(r.Context(), ws, mux.Vars(r)["id"], bucket)
	if err != nil {
		writeError(w, err, "campaign timeline")
		return
	}
	points := timeline.Points
	if points == nil {
		points = []campaign.TimelinePoint{}
	}
	response.WriteSuccess(w, http.StatusOK, TimelineDTO{Bucket: string(timeline.Bucket), Points: points})
}

// @Summary		Destinatários da campanha
// @Tags			RCS e SMS
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Param			status	query		string	false	"pending, sent, skipped ou failed"
// @Param			run_id	query		string	false	"Execução de uma campanha recorrente"
// @Param			page	query		int		false	"Página"
// @Param			limit	query		int		false	"Itens por página, até 100"
// @Success		200		{object}	RecipientListResponse
// @Router			/carrier/campaigns/{id}/recipients [get]
func (h *Handler) Recipients(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	page, ok := pageOf(q)
	if !ok {
		writeError(w, errQueryInvalid, "campaign recipients")
		return
	}
	query := ccuc.RecipientQuery{
		RunID: strings.TrimSpace(q.Get("run_id")), Status: cc.RecipientStatus(strings.TrimSpace(q.Get("status"))), Pagination: page,
	}
	result, err := h.api.Recipients(r.Context(), ws, mux.Vars(r)["id"], query)
	if err != nil {
		writeError(w, err, "campaign recipients")
		return
	}
	out := RecipientListResponse{Items: make([]RecipientDTO, 0, len(result.Items)), Total: result.TotalItems}
	for _, row := range result.Items {
		out.Items = append(out.Items, recipientDTO(row))
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Visão geral de RCS e SMS
// @Description	Totais, taxas, volume diário, custo por conversão por canal comparado ao WhatsApp e entrega por operadora.
// @Tags			RCS e SMS
// @Produce		json
// @Param			days	query		int		false	"7, 30 ou 90"
// @Param			channel	query		string	false	"all, rcs ou sms"
// @Success		200		{object}	OverviewDTO
// @Router			/carrier/overview [get]
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	ws, ok := httpx.RequireWorkspace(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	days, ok := intParam(q, "days", defaultOverviewDays)
	if !ok {
		writeError(w, errQueryInvalid, "overview")
		return
	}
	channel := strings.TrimSpace(q.Get("channel"))
	if channel == "" {
		channel = allChannels
	}
	o, err := h.api.Overview(r.Context(), ws, days, channel)
	if err != nil {
		writeError(w, err, "overview")
		return
	}
	response.WriteSuccess(w, http.StatusOK, overviewDTO(o))
}
