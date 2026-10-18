package buttonactionhttp

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/buttonaction"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	buttonaction_usecase "vozko/usecases/buttonaction"
)

type Service interface {
	TemplateActions(ctx context.Context, a buttonaction_usecase.Actor, templateID string) (*buttonaction_usecase.TemplateView, error)
	SaveTemplateActions(ctx context.Context, a buttonaction_usecase.Actor, templateID string, expectedVersion int64, change buttonaction_usecase.TemplateChange) (*buttonaction_usecase.TemplateView, error)
	ExitVerdict(ctx context.Context, a buttonaction_usecase.Actor, templateID, campaignID string) (*buttonaction_usecase.VerdictView, error)
	DraftExitVerdict(ctx context.Context, a buttonaction_usecase.Actor, draft buttonaction_usecase.DraftTemplate) (*buttonaction_usecase.DraftVerdictView, error)
	CampaignRoutes(ctx context.Context, a buttonaction_usecase.Actor, campaignID string) (*buttonaction_usecase.CampaignView, error)
	SaveCampaignRoutes(ctx context.Context, a buttonaction_usecase.Actor, campaignID string, expectedVersion int64, inputs []buttonaction.Input) (*buttonaction_usecase.CampaignView, error)
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) actor(w http.ResponseWriter, r *http.Request) (buttonaction_usecase.Actor, bool) {
	if h.service == nil {
		WriteRefusal(w, buttonaction.ErrUnavailable)
		return buttonaction_usecase.Actor{}, false
	}
	viewer, ok := httpx.WorkspaceActor(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, "authentication required", nil)
		return buttonaction_usecase.Actor{}, false
	}
	return buttonaction_usecase.Actor{
		WorkspaceID: viewer.WorkspaceID, UserID: viewer.UserID, IsAdmin: viewer.IsAdmin, Departments: middleware.GetDepartmentFilter(r),
	}, true
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if errors.Is(err, shared.ErrVersionRequired) {
		httpx.WriteVersionRequired(w)
		return
	}
	if WriteRefusal(w, err) {
		return
	}
	log.Printf("[button-actions] %v", err)
	response.WriteError(w, http.StatusInternalServerError, "Failed to handle the button actions", nil)
}

// @Summary		Ações dos botões e saída do modelo
// @Description	Mostra o que cada botão de resposta rápida do modelo faz quando o lead toca nele e a confirmação de saída do modelo. São dados do Vozko: salvá-los nunca reenvia o modelo para a Meta. `buttons` lista cada botão de resposta rápida (`index`, `text`) com a ação que vale hoje (`action`, ou null) e de onde ela vem (`source`); uma ação cujo botão mudou de texto na Meta aparece com `stale: true` e não roda. `actions` traz as ações gravadas, inclusive as desatualizadas. `allowedKinds` lista os tipos de ação que quem pede pode configurar, pelas mesmas permissões que o salvamento exige (por exemplo, `consent_grant` só com `leads.consent`). Envie `version` no cabeçalho If-Match ao salvar.
// @Tags			Modelos de WhatsApp
// @Produce		json
// @Param			id	path		string	true	"ID do modelo"
// @Success		200	{object}	TemplateActionsResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id}/button-actions [get]
func (h *Handler) GetTemplateActions(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	view, err := h.service.TemplateActions(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentTemplateActions(view))
}

// @Summary		Salvar ações dos botões e saída do modelo
// @Description	Substitui as ações dos botões de resposta rápida e a confirmação de saída do modelo, a partir da `version` lida (If-Match; 428 `version_required` sem ela, 409 `version_conflict` com o estado atual em `current`). Nunca reenvia o modelo para a Meta. Cada ação vale para um botão de resposta rápida (`buttonIndex`) e tem um tipo: `opt_out` (sem parâmetros), `consent_grant` (`purpose`), `hand_off` (`departmentId` para a roleta da equipe ou `userId` para um membro, exatamente um), `start_workflow` (`workflowId`), `call_list` (`callListId`), `classify` (`fieldKey` e `option` de um campo de seleção do lead) e `create_deal` (`pipelineId` e `stageId`). Além de gerenciar modelos, cada tipo novo, alterado ou removido exige a permissão dele: `leads.consent` para `consent_grant`, `leads.assign` para `hand_off`, `call_lists.manage` para `call_list`, `leads:update` para `classify`, `workflows.edit` para `start_workflow` e `deals.edit` para `create_deal` (403 `button_action_forbidden`). As palavras de saída do modelo não são gravadas aqui: são os comandos `/palavra` que o próprio texto do modelo mostra ao lead (cabeçalho de texto, corpo, rodapé ou texto de um botão de resposta rápida). A confirmação de saída é enviada quando o lead usa uma delas; sem confirmação, nenhuma confirmação é enviada. Cada ação leva `buttonText`, o texto do botão que a pessoa viu ao salvar: com o texto atual do botão, a ação vale para ele; uma ação desatualizada reenviada sem nenhuma mudança e com o texto que ela guarda continua gravada e desatualizada; qualquer outro texto é recusado com 422 `button_action_stale`, e nada é gravado. `purpose` de `consent_grant` é opcional, até 200 caracteres. Códigos: 400 `button_action_kind_unknown`, `button_action_not_quick_reply`, `button_action_params_invalid`, `button_action_confirmation_too_long`, `button_action_duplicate_button`, `exit_confirmation_too_long`; 422 `button_action_params_not_found`, `button_action_stale`; 404 `button_action_template_not_found`.
// @Tags			Modelos de WhatsApp
// @Accept			json
// @Produce		json
// @Param			id			path		string						true	"ID do modelo"
// @Param			If-Match	header		string						true	"version lida"
// @Param			body		body		SaveTemplateActionsRequest	true	"ações e saída"
// @Success		200			{object}	TemplateActionsResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		409			{object}	httpx.VersionConflict{current=TemplateActionsResponse}
// @Failure		422			{object}	response.ErrorResponse
// @Failure		428			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id}/button-actions [put]
func (h *Handler) SaveTemplateActions(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	version, ok := httpx.IfMatchVersion(w, r)
	if !ok {
		return
	}
	var body SaveTemplateActionsRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	id := mux.Vars(r)["id"]
	view, err := h.service.SaveTemplateActions(r.Context(), a, id, version, buttonaction_usecase.TemplateChange{
		Actions: httpx.ButtonActionInputs(body.Actions), ExitConfirmation: body.ExitConfirmation,
	})
	if errors.Is(err, buttonaction.ErrVersionConflict) {
		current, readErr := h.service.TemplateActions(r.Context(), a, id)
		if readErr != nil {
			writeError(w, readErr)
			return
		}
		httpx.WriteVersionConflict(w, "As ações do modelo foram salvas em outro lugar", presentTemplateActions(current))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentTemplateActions(view))
}

// @Summary		Saída do lead no modelo
// @Description	O veredito que a tela de nova campanha mostra na seção "Saída do lead", calculado só com o que o lead vê no modelo: `ok` quando o modelo tem um botão de resposta rápida com ação de saída (`opt_out` ou `consent_grant`) ou mostra um comando `/palavra` no texto (cabeçalho de texto, corpo, rodapé ou texto de um botão de resposta rápida); `none` nos outros casos. Palavras guardadas só nas configurações nunca contam, porque o lead não as vê. Traz cada botão de resposta rápida com a ação que vale (com `campaignId`, o desvio da campanha aparece com `source` `campaign`) e as palavras que o modelo mostra (`exitWords`, sem a barra). `campaignAcknowledgementRequired` é true quando criar ou trocar para este modelo numa campanha exige `exitRiskAcknowledged` (veredito `none`, qualquer categoria): é o que a tela de campanha e os envios pela tela de leads usam. `templateExitRequired` é true quando um modelo assim não pode ser criado (só marketing com veredito `none`): é o que a página do modelo usa para o aviso. As duas regras são as mesmas dos bloqueios no servidor.
// @Tags			Modelos de WhatsApp
// @Produce		json
// @Param			id			path		string	true	"ID do modelo"
// @Param			campaignId	query		string	false	"campanha cujos desvios entram na lista"
// @Success		200			{object}	ExitVerdictResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id}/exit-verdict [get]
func (h *Handler) GetExitVerdict(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	view, err := h.service.ExitVerdict(r.Context(), a, mux.Vars(r)["id"], r.URL.Query().Get("campaignId"))
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentVerdict(&view.Verdict, view.Gates, view.Template))
}

// @Summary		Saída do lead num modelo em rascunho
// @Description	O mesmo veredito de `GET /whatsapp/templates/{id}/exit-verdict`, calculado sobre um modelo que ainda não existe: os textos do rascunho (`header` quando o cabeçalho é de texto, `body` e `footer`), os botões (`buttons`, na ordem do componente de botões) e as ações dos botões de resposta rápida (`buttonActions`, com `buttonText` igual ao texto do botão no rascunho). As palavras de saída são os comandos `/palavra` que esses textos mostram. Nada é gravado e nada vai para a Meta. `verdict.templateExitRequired` é true quando o modelo não pode ser criado assim (marketing com veredito `none`, recusado com 422 `marketing_template_needs_exit`); `verdict.campaignAcknowledgementRequired` é true quando uma campanha com esse modelo exigiria o reconhecimento (veredito `none`, qualquer categoria). A tela de novo modelo usa o primeiro. `allowedKinds` lista os tipos de ação que quem pede pode configurar, pela mesma regra de permissões do salvamento, para o construtor oferecer só esses. Recusas com os mesmos códigos de salvar ações: 400 `button_action_kind_unknown`, `button_action_not_quick_reply`, `button_action_params_invalid`, `button_action_confirmation_too_long`, `button_action_duplicate_button`; 422 `button_action_stale` quando `buttonText` não é o texto do botão no rascunho.
// @Tags			Modelos de WhatsApp
// @Accept			json
// @Produce		json
// @Param			body	body		DraftExitVerdictRequest	true	"rascunho do modelo"
// @Success		200		{object}	DraftExitVerdictResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/exit-verdict [post]
func (h *Handler) DraftExitVerdict(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	var body DraftExitVerdictRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	view, err := h.service.DraftExitVerdict(r.Context(), a, body.draft())
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, DraftExitVerdictResponse{
		Verdict: presentVerdict(&view.Verdict, view.Gates, view.Template), AllowedKinds: presentKinds(view.AllowedKinds),
	})
}

// @Summary		Desvios dos botões na campanha
// @Description	Onde cada botão do modelo leva nesta campanha. A campanha só pode mudar o destino de um botão que não é de saída; a saída é sempre a do modelo. Traz também o reconhecimento de risco gravado na criação, quando houve.
// @Tags			Campanhas de WhatsApp
// @Produce		json
// @Param			id	path		string	true	"ID da campanha"
// @Success		200	{object}	CampaignRoutesResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaigns/{id}/button-routes [get]
func (h *Handler) GetCampaignRoutes(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	view, err := h.service.CampaignRoutes(r.Context(), a, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentCampaignRoutes(view))
}

// @Summary		Salvar desvios dos botões na campanha
// @Description	Substitui os desvios da campanha a partir da `version` lida (If-Match). Só vale para botões de resposta rápida do modelo da campanha e só com tipos que não são de saída; `opt_out`, `consent_grant` ou um botão cuja ação no modelo é de saída são recusados com 422 `button_action_exit_locked`. Cada tipo exige a mesma permissão do modelo. Cada desvio leva `buttonText`, o texto do botão que a pessoa viu; um texto que o botão não tem mais é recusado com 422 `button_action_stale`. Ordem de resolução no toque: a saída do modelo, depois o desvio da campanha, depois a ação do modelo.
// @Tags			Campanhas de WhatsApp
// @Accept			json
// @Produce		json
// @Param			id			path		string						true	"ID da campanha"
// @Param			If-Match	header		string						true	"version lida"
// @Param			body		body		SaveCampaignRoutesRequest	true	"desvios"
// @Success		200			{object}	CampaignRoutesResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		409			{object}	httpx.VersionConflict{current=CampaignRoutesResponse}
// @Failure		422			{object}	response.ErrorResponse
// @Failure		428			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/campaigns/{id}/button-routes [put]
func (h *Handler) SaveCampaignRoutes(w http.ResponseWriter, r *http.Request) {
	a, ok := h.actor(w, r)
	if !ok {
		return
	}
	version, ok := httpx.IfMatchVersion(w, r)
	if !ok {
		return
	}
	var body SaveCampaignRoutesRequest
	if !httpx.DecodeStrictJSON(w, r, &body) {
		return
	}
	id := mux.Vars(r)["id"]
	view, err := h.service.SaveCampaignRoutes(r.Context(), a, id, version, httpx.ButtonActionInputs(body.Routes))
	if errors.Is(err, buttonaction.ErrVersionConflict) {
		current, readErr := h.service.CampaignRoutes(r.Context(), a, id)
		if readErr != nil {
			writeError(w, readErr)
			return
		}
		httpx.WriteVersionConflict(w, "Os desvios da campanha foram salvos em outro lugar", presentCampaignRoutes(current))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentCampaignRoutes(view))
}
