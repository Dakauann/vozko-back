package smartcampaignhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
	smartcampaign_usecase "vozko/usecases/smartcampaign"
)

type Campaigns interface {
	Owned(workspaceID string, departments *wd.DepartmentFilter, campaignID string) (*wc.Campaign, error)
}

type Overview interface {
	Overview(campaignID string) (*smartcampaign_usecase.View, error)
}

type Settings interface {
	Save(target *wc.Campaign, policy sc.Policy) error
	Remove(target *wc.Campaign) error
	Defaults(ctx context.Context, target *wc.Campaign) (smartcampaign_usecase.Defaults, error)
}

type Handler struct {
	campaigns Campaigns
	overview  Overview
	settings  Settings
}

func NewHandler(campaigns Campaigns, overview Overview, settings Settings) *Handler {
	return &Handler{campaigns: campaigns, overview: overview, settings: settings}
}

// @Summary		Disparo inteligente da campanha
// @Description	Mostra a política de ondas da campanha (ou null quando a campanha envia tudo de uma vez), os padrões para preencher o formulário (horário de envio do departamento ou do workspace e os três perfis), o que acontece agora e a próxima hora prevista, e o histórico de ondas com o resultado e a decisão de cada uma.
// @Description
// @Description	| progress.phase | Significado |
// @Description	|---|---|
// @Description	| not_started | A campanha ainda não começou |
// @Description	| sending | Uma onda está saindo, ou a próxima sai em `until` |
// @Description	| observing | A onda saiu e está em observação até `until` |
// @Description	| waiting | Aguardando até `until` (horário de envio, limite diário ou leitura de saúde) |
// @Description	| held | Pausada ou encerrada automaticamente, com o motivo em `reason` |
// @Description	| paused, stopped | Pausada ou parada por uma pessoa |
// @Description	| completed | Todos os contatos foram processados |
// @Tags			WhatsApp Campaigns
// @Produce		json
// @Param			id	path		string	true	"ID da campanha"
// @Success		200	{object}	Response
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaigns/{id}/smart [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	target, ok := h.owned(w, r)
	if !ok {
		return
	}
	h.writeView(w, r, target)
}

// @Summary		Salvar o disparo inteligente da campanha
// @Description	Liga ou ajusta o envio em ondas. Só é aceito quando a campanha não está em execução. O horário de envio é obrigatório; tempos em minutos e taxas em fração (0,08 = 8%). Uma política que enfraqueça as regras de proteção é recusada com 400.
// @Tags			WhatsApp Campaigns
// @Accept			json
// @Produce		json
// @Param			id		path		string		true	"ID da campanha"
// @Param			body	body		PolicyBody	true	"Política de ondas"
// @Success		200		{object}	Response
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaigns/{id}/smart [put]
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	target, ok := h.owned(w, r)
	if !ok {
		return
	}
	var body PolicyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"preset": "conservative | standard | aggressive", "sendHours": "working hours"})
		return
	}
	policy, err := body.ToDomain()
	if err == nil {
		err = h.settings.Save(target, policy)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	h.writeView(w, r, target)
}

// @Summary		Desligar o disparo inteligente da campanha
// @Description	Remove a política de ondas: ao iniciar ou retomar, a campanha volta a enviar todos os contatos de uma vez. Só é aceito quando a campanha não está em execução.
// @Tags			WhatsApp Campaigns
// @Produce		json
// @Param			id	path		string	true	"ID da campanha"
// @Success		200	{object}	Response
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaigns/{id}/smart [delete]
func (h *Handler) Remove(w http.ResponseWriter, r *http.Request) {
	target, ok := h.owned(w, r)
	if !ok {
		return
	}
	if err := h.settings.Remove(target); err != nil {
		writeError(w, err)
		return
	}
	h.writeView(w, r, target)
}

func (h *Handler) owned(w http.ResponseWriter, r *http.Request) (*wc.Campaign, bool) {
	id := strings.TrimSpace(mux.Vars(r)["id"])
	target, err := h.campaigns.Owned(middleware.GetWorkspaceID(r), middleware.GetDepartmentFilter(r), id)
	if err != nil || target == nil {
		response.WriteError(w, http.StatusNotFound, wc.ErrCampaignNotFound.Error(), nil)
		return nil, false
	}
	return target, true
}

func (h *Handler) writeView(w http.ResponseWriter, r *http.Request, target *wc.Campaign) {
	view, err := h.overview.Overview(target.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	defaults, err := h.settings.Defaults(r.Context(), target)
	if err != nil {
		writeError(w, err)
		return
	}
	out := Response{
		Editable: target.Status != campaign.StatusRunning,
		Defaults: defaultsBody(defaults),
		Progress: view.Progress,
		Waves:    view.Waves,
	}
	if view.Policy != nil {
		body := policyBody(*view.Policy)
		out.Policy = &body
	}
	if out.Waves == nil {
		out.Waves = []sc.Wave{}
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sc.ErrInvalidPolicy):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, sc.ErrPolicyLocked):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "could not read the smart campaign", nil)
	}
}

// @Summary		Padrões do disparo inteligente para uma nova campanha
// @Description	Os três perfis de ondas e o horário de envio pré-preenchido (o do departamento informado ou, sem ele, o do workspace), para o formulário de criação.
// @Tags			WhatsApp Campaigns
// @Produce		json
// @Param			departmentId	query		string	false	"ID do departamento da nova campanha"
// @Success		200				{object}	DefaultsBody
// @Failure		403				{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaign-waves/defaults [get]
func (h *Handler) Defaults(w http.ResponseWriter, r *http.Request) {
	departmentID := strings.TrimSpace(r.URL.Query().Get("departmentId"))
	if departmentID != "" && !httpx.CanAccessDepartment(r, departmentID) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this department", nil)
		return
	}
	defaults, err := h.settings.Defaults(r.Context(), &wc.Campaign{WorkspaceID: middleware.GetWorkspaceID(r), DepartmentID: departmentID})
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, defaultsBody(defaults))
}

func defaultsBody(defaults smartcampaign_usecase.Defaults) DefaultsBody {
	body := DefaultsBody{SendHours: defaults.SendHours, Presets: make(map[string]PolicyBody, len(defaults.Presets))}
	for preset, policy := range defaults.Presets {
		body.Presets[string(preset)] = policyBody(policy)
	}
	return body
}
