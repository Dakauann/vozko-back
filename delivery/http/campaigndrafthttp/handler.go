package campaigndrafthttp

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	"vozko/domain/campaigndraft"
	"vozko/infra/http/middleware"
	campaigndraft_usecase "vozko/usecases/campaigndraft"
)

type Finisher interface {
	Review(w http.ResponseWriter, r *http.Request, request json.RawMessage)
	Launch(w http.ResponseWriter, r *http.Request, request json.RawMessage, launch campaign.Launch, confirmedFingerprint string) bool
}

type Guard func(http.HandlerFunc) http.HandlerFunc

type Handler struct {
	drafts   *campaigndraft_usecase.Service
	channel  campaigndraft.Channel
	finisher Finisher
	start    Guard
}

func NewHandler(drafts *campaigndraft_usecase.Service, channel campaigndraft.Channel, finisher Finisher) *Handler {
	return &Handler{drafts: drafts, channel: channel, finisher: finisher}
}

type DraftResponse struct {
	ID        string          `json:"id"`
	Channel   string          `json:"channel" example:"whatsapp"`
	Name      string          `json:"name"`
	Payload   json.RawMessage `json:"payload" swaggertype:"object"`
	CreatedBy string          `json:"createdBy"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type DraftListResponse struct {
	Items []DraftResponse `json:"items"`
}

type SaveRequest struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload" swaggertype:"object"`
}

type LaunchRequest struct {
	Mode                 string     `json:"mode" enums:"now,schedule,ready" example:"now"`
	ScheduledStart       *time.Time `json:"scheduledStart,omitempty"`
	ConfirmedFingerprint string     `json:"confirmedFingerprint,omitempty" example:"3f2a9c1b7d4e8a60"`
}

func toResponse(d *campaigndraft.Draft) DraftResponse {
	payload := d.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	return DraftResponse{ID: d.ID, Channel: string(d.Channel), Name: d.Name, Payload: payload, CreatedBy: d.CreatedBy, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
}

func (h *Handler) scope(w http.ResponseWriter, r *http.Request) (campaigndraft_usecase.Scope, bool) {
	claims := middleware.GetClaims(r)
	workspaceID := strings.TrimSpace(middleware.GetWorkspaceID(r))
	if claims == nil || strings.TrimSpace(claims.UserID) == "" || workspaceID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return campaigndraft_usecase.Scope{}, false
	}
	if h.drafts == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Campaign drafts are not available", nil)
		return campaigndraft_usecase.Scope{}, false
	}
	return campaigndraft_usecase.Scope{WorkspaceID: workspaceID, UserID: claims.UserID}, true
}

var refusals = []response.ErrorRefusal{
	{Err: campaigndraft.ErrNotFound, Refusal: response.Refusal{Status: http.StatusNotFound, Code: "campaign_draft_not_found", Message: "Campaign draft not found"}},
	{Err: campaigndraft.ErrTooLarge, Refusal: response.Refusal{Status: http.StatusRequestEntityTooLarge, Code: "campaign_draft_too_large", Message: "The draft is larger than 1 MB"}},
	{Err: campaigndraft.ErrIncomplete, Refusal: response.Refusal{Status: http.StatusUnprocessableEntity, Code: "campaign_draft_incomplete", Message: "The draft has no campaign yet"}},
	{Err: campaigndraft.ErrInvalid, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "campaign_draft_invalid", Message: "The draft is invalid"}},
	{Err: campaign.ErrLaunchModeInvalid, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "campaign_launch_mode_invalid", Message: "The mode must be now, schedule or ready"}},
	{Err: campaign.ErrLaunchScheduleMissing, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "campaign_launch_schedule_missing", Message: "Scheduling needs a date and time"}},
}

func Refusals() []response.Refusal {
	return response.ErrorRefusalsOf(refusals)
}

func writeError(w http.ResponseWriter, err error) {
	if response.WriteErrorRefusal(w, err, refusals) {
		return
	}
	response.WriteError(w, http.StatusInternalServerError, "Failed to handle the campaign draft", nil)
}

func decodeSave(w http.ResponseWriter, r *http.Request) (SaveRequest, bool) {
	var body SaveRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, campaigndraft.MaxPayloadBytes+64*1024)).Decode(&body); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"name": "string", "payload": "object"})
		return SaveRequest{}, false
	}
	return body, true
}

// @Summary		Listar rascunhos de campanha
// @Description	Rascunhos salvos pelo assistente de criação do canal, do mais recente ao mais antigo (até 100). Um rascunho nunca envia, nunca entra em contagens, métricas, ondas, agendamentos ou verificações de início: ele vira campanha só pelo POST /campaign-drafts/{channel}/{id}/launch.
// @Tags			Rascunhos de campanha
// @Produce		json
// @Param			channel	path		string	true	"whatsapp ou unofficial_whatsapp"
// @Success		200		{object}	campaigndrafthttp.DraftListResponse
// @Failure		401		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/campaign-drafts/{channel} [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	drafts, err := h.drafts.List(r.Context(), scope, h.channel)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]DraftResponse, 0, len(drafts))
	for i := range drafts {
		items = append(items, toResponse(&drafts[i]))
	}
	response.WriteSuccess(w, http.StatusOK, DraftListResponse{Items: items})
}

// @Summary		Criar um rascunho de campanha
// @Description	Salva o que já foi preenchido, sem validar a campanha: só o canal, o nome (até 255 caracteres) e `payload`, um objeto JSON de até 1 MB com `request` (o corpo de criação da campanha do canal) e `state` (estado do assistente). As verificações completas rodam em /review e /launch.
// @Tags			Rascunhos de campanha
// @Accept			json
// @Produce		json
// @Param			channel	path		string								true	"whatsapp ou unofficial_whatsapp"
// @Param			body	body		campaigndrafthttp.SaveRequest		true	"Rascunho"
// @Success		201		{object}	campaigndrafthttp.DraftResponse
// @Failure		400		{object}	response.ErrorResponse	"campaign_draft_invalid"
// @Failure		413		{object}	response.ErrorResponse	"campaign_draft_too_large"
// @Security		BearerAuth
// @Router			/campaign-drafts/{channel} [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	body, ok := decodeSave(w, r)
	if !ok {
		return
	}
	d, err := h.drafts.Create(r.Context(), scope, h.channel, body.Name, body.Payload)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toResponse(d))
}

// @Summary		Ler um rascunho de campanha
// @Tags			Rascunhos de campanha
// @Produce		json
// @Param			channel	path		string	true	"whatsapp ou unofficial_whatsapp"
// @Param			id		path		string	true	"ID do rascunho"
// @Success		200		{object}	campaigndrafthttp.DraftResponse
// @Failure		404		{object}	response.ErrorResponse	"campaign_draft_not_found"
// @Security		BearerAuth
// @Router			/campaign-drafts/{channel}/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	d, err := h.drafts.Get(r.Context(), scope, h.channel, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponse(d))
}

// @Summary		Salvar um rascunho de campanha
// @Description	Substitui o nome e o `payload` do rascunho (salvamento automático do assistente). Mesmas regras leves da criação.
// @Tags			Rascunhos de campanha
// @Accept			json
// @Produce		json
// @Param			channel	path		string							true	"whatsapp ou unofficial_whatsapp"
// @Param			id		path		string							true	"ID do rascunho"
// @Param			body	body		campaigndrafthttp.SaveRequest	true	"Rascunho"
// @Success		200		{object}	campaigndrafthttp.DraftResponse
// @Failure		400		{object}	response.ErrorResponse	"campaign_draft_invalid"
// @Failure		404		{object}	response.ErrorResponse	"campaign_draft_not_found"
// @Failure		413		{object}	response.ErrorResponse	"campaign_draft_too_large"
// @Security		BearerAuth
// @Router			/campaign-drafts/{channel}/{id} [put]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	body, ok := decodeSave(w, r)
	if !ok {
		return
	}
	d, err := h.drafts.Update(r.Context(), scope, h.channel, mux.Vars(r)["id"], body.Name, body.Payload)
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResponse(d))
}

// @Summary		Excluir um rascunho de campanha
// @Tags			Rascunhos de campanha
// @Produce		json
// @Param			channel	path		string	true	"whatsapp ou unofficial_whatsapp"
// @Param			id		path		string	true	"ID do rascunho"
// @Success		200		{object}	map[string]bool
// @Failure		404		{object}	response.ErrorResponse	"campaign_draft_not_found"
// @Security		BearerAuth
// @Router			/campaign-drafts/{channel}/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.scope(w, r)
	if !ok {
		return
	}
	if err := h.drafts.Delete(r.Context(), scope, h.channel, mux.Vars(r)["id"]); err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *Handler) request(w http.ResponseWriter, r *http.Request) (*campaigndraft.Draft, json.RawMessage, bool) {
	scope, ok := h.scope(w, r)
	if !ok {
		return nil, nil, false
	}
	if h.finisher == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Campaign drafts cannot be finished on this channel", nil)
		return nil, nil, false
	}
	d, err := h.drafts.Get(r.Context(), scope, h.channel, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err)
		return nil, nil, false
	}
	request, err := d.Request()
	if err != nil {
		writeError(w, err)
		return nil, nil, false
	}
	return d, request, true
}

// @Summary		Revisar um rascunho antes do envio
// @Description	Calcula no servidor, a partir do rascunho salvo, o público (a mesma contagem do passo de público), o custo (oficial: `quote`, com `unitPriceBrl`, `costBrl` e `balanceBrl` já em reais pela cotação da carteira, ao lado dos micros em `currency`; não oficial: `estimate`) e a verificação de início em `check` (`findings`, `fingerprint`, `blocks`, `needsConfirmation`), com as mesmas regras do Iniciar. Nada é criado.
// @Tags			Rascunhos de campanha
// @Produce		json
// @Param			channel	path		string	true	"whatsapp ou unofficial_whatsapp"
// @Param			id		path		string	true	"ID do rascunho"
// @Success		200		{object}	map[string]interface{}
// @Failure		404		{object}	response.ErrorResponse	"campaign_draft_not_found"
// @Failure		422		{object}	response.ErrorResponse	"campaign_draft_incomplete e códigos do público"
// @Security		BearerAuth
// @Router			/campaign-drafts/{channel}/{id}/review [post]
func (h *Handler) Review(w http.ResponseWriter, r *http.Request) {
	_, request, ok := h.request(w, r)
	if !ok {
		return
	}
	h.finisher.Review(w, r, request)
}

// @Summary		Concluir um rascunho: enviar agora, agendar ou deixar pronta
// @Description	Um único pedido cria a campanha do rascunho e decide o início. `mode: ready` cria a campanha parada, pronta para iniciar quando quiser. `mode: schedule` cria com `scheduledStart` (o agendamento de sempre; no horário o servidor confere tudo de novo). `mode: now` exige a permissão de iniciar, roda antes a mesma verificação do Iniciar sobre o rascunho e, se ela bloqueia (409 `campaign_launch_blocked`) ou pede confirmação (409 `campaign_launch_unconfirmed`, envie `confirmedFingerprint`), nada é criado e o rascunho continua; senão cria, aplica as configurações do rascunho e inicia. Criada a campanha, o rascunho é apagado. Se o início ainda recusar depois de criar, a resposta traz a campanha pronta com `started: false` e `startRefusal`.
// @Tags			Rascunhos de campanha
// @Accept			json
// @Produce		json
// @Param			channel	path		string							true	"whatsapp ou unofficial_whatsapp"
// @Param			id		path		string							true	"ID do rascunho"
// @Param			body	body		campaigndrafthttp.LaunchRequest	true	"Como iniciar"
// @Success		201		{object}	map[string]interface{}
// @Failure		400		{object}	response.ErrorResponse	"campaign_launch_mode_invalid, campaign_launch_schedule_missing e códigos de validação da campanha"
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse	"campaign_draft_not_found"
// @Failure		409		{object}	response.ErrorResponse	"campaign_launch_blocked, campaign_launch_unconfirmed"
// @Failure		422		{object}	response.ErrorResponse	"campaign_draft_incomplete e códigos do público"
// @Security		BearerAuth
// @Router			/campaign-drafts/{channel}/{id}/launch [post]
func (h *Handler) Launch(w http.ResponseWriter, r *http.Request) {
	var body LaunchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"mode": "now, schedule or ready"})
		return
	}
	launch, err := campaign.ParseLaunch(body.Mode, body.ScheduledStart)
	if err != nil {
		writeError(w, err)
		return
	}
	finish := func(w http.ResponseWriter, r *http.Request) {
		d, request, ok := h.request(w, r)
		if !ok {
			return
		}
		if !h.finisher.Launch(w, r, request, launch, body.ConfirmedFingerprint) {
			return
		}
		scope, _ := h.scope(w, r)
		if err := h.drafts.Delete(r.Context(), scope, h.channel, d.ID); err != nil && !errors.Is(err, campaigndraft.ErrNotFound) {
			log.Printf("[campaign-drafts] draft %s became a campaign but could not be deleted: %v", d.ID, err)
		}
	}
	if launch.StartsNow() {
		if h.start == nil {
			response.WriteError(w, http.StatusForbidden, "You cannot start campaigns", nil)
			return
		}
		h.start(finish)(w, r)
		return
	}
	finish(w, r)
}
