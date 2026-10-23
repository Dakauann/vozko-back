package choicestatshttp

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	unofficialwahttp "vozko/delivery/http/unofficial_whatsapp"
	"vozko/domain/choicestats"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workflow"
	"vozko/infra/http/middleware"
)

type Reader interface {
	Workflow(ctx context.Context, scope workflow.Scope, workflowID, from, to string, version *int) (*choicestats.WorkflowReport, error)
	WhatsAppCampaign(ctx context.Context, access wc.ReportAccess, campaignID, from, to string) (*choicestats.PromptReport, error)
	UnofficialCampaign(ctx context.Context, workspaceID string, scope uw.DepartmentScope, campaignID, from, to string) (*choicestats.PromptReport, error)
	Template(ctx context.Context, workspaceID, templateID, from, to string) (*choicestats.PromptReport, error)
}

var (
	refusalInvalidPeriod   = response.Refusal{Status: http.StatusBadRequest, Code: "invalid_period", Message: "Período inválido: use from e to no formato AAAA-MM-DD, from até to, com no máximo 400 dias"}
	refusalInvalidVersion  = response.Refusal{Status: http.StatusBadRequest, Code: "invalid_version", Message: "Versão inválida: use um número de versão publicada"}
	refusalCampaignMissing = response.Refusal{Status: http.StatusNotFound, Code: "campaign_not_found", Message: "Campanha não encontrada"}
	refusalTemplateMissing = response.Refusal{Status: http.StatusNotFound, Code: "template_not_found", Message: "Template não encontrado"}
	refusalUnavailable     = response.Refusal{Status: http.StatusServiceUnavailable, Code: "choice_stats_unavailable", Message: "Não foi possível ler os cliques agora. Tente de novo em instantes."}
)

var readRefusals = append(append([]response.ErrorRefusal{}, httpx.WorkflowAccessRefusals...),
	response.ErrorRefusal{Err: choicestats.ErrInvalidPeriod, Refusal: refusalInvalidPeriod},
	response.ErrorRefusal{Err: choicestats.ErrInvalidVersion, Refusal: refusalInvalidVersion},
	response.ErrorRefusal{Err: wc.ErrCampaignNotFound, Refusal: refusalCampaignMissing},
	response.ErrorRefusal{Err: wc.ErrCampaignAccessDenied, Refusal: refusalCampaignMissing},
	response.ErrorRefusal{Err: uwc.ErrCampaignNotFound, Refusal: refusalCampaignMissing},
	response.ErrorRefusal{Err: template.ErrTemplateNotFound, Refusal: refusalTemplateMissing},
	response.ErrorRefusal{Err: template.ErrTemplateAccessDenied, Refusal: refusalTemplateMissing},
	response.ErrorRefusal{Err: choicestats.ErrUnavailable, Refusal: refusalUnavailable},
)

var requestRefusals = []response.Refusal{refusalUnavailable, refusalCampaignMissing}

type Handler struct {
	reader      Reader
	departments unofficialwahttp.DepartmentScopeResolver
}

func NewHandler(reader Reader, departments unofficialwahttp.DepartmentScopeResolver) *Handler {
	return &Handler{reader: reader, departments: departments}
}

func writeReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if !response.WriteErrorRefusal(w, err, readRefusals) {
		response.WriteRefusal(w, refusalUnavailable)
	}
}

func period(r *http.Request) (string, string) {
	q := r.URL.Query()
	return strings.TrimSpace(q.Get("from")), strings.TrimSpace(q.Get("to"))
}

func versionOf(r *http.Request) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("version"))
	if raw == "" {
		return nil, nil
	}
	version, err := strconv.Atoi(raw)
	if err != nil || version <= 0 {
		return nil, choicestats.ErrInvalidVersion
	}
	return &version, nil
}

// @Summary		Cliques nas opções de um fluxo
// @Description	Taxa de cliques (CTR) de cada nó que envia escolhas no fluxo: "Enviar Opções" (botões ou lista) e templates com botões. sends conta os envios aceitos pelo provedor (um envio retido pela janela fechada não conta); clickedPrompts conta os envios com pelo menos um clique, e só o primeiro clique de cada envio vale. Cada opção traz shown (quantas vezes o canal exibiu a opção; opções que o canal cortou por limite não contam), clicks e ctr. freeText conta as respostas digitadas que o fluxo encaminhou como sem correspondência (texto igual ao título de uma opção também é texto livre). Taxas em pontos-base (0 a 10000). Os cliques contam no dia do envio, no fuso do workspace, por até 30 dias depois do envio. Sem version, soma todas as versões do fluxo; as opções seguem as saídas desenhadas no rascunho. Nós sem envios no período não aparecem. Execuções de teste (simulação e teste do nó) nunca contam. Exige `workflows:read`.
// @Tags			workflows
// @Produce		json
// @Param			id		path		string	true	"ID do workflow"
// @Param			from	query		string	true	"Primeiro dia (AAAA-MM-DD), no fuso do workspace"
// @Param			to		query		string	true	"Último dia (AAAA-MM-DD), no máximo 400 dias após from"
// @Param			version	query		int		false	"Só a versão publicada informada"
// @Success		200		{object}	choicestats.WorkflowReport
// @Failure		400		{object}	response.ErrorResponse	"invalid_period ou invalid_version"
// @Failure		403		{object}	response.ErrorResponse	"workflow_access_denied: workflow de outro departamento"
// @Failure		404		{object}	response.ErrorResponse	"workflow_not_found"
// @Failure		503		{object}	response.ErrorResponse	"choice_stats_unavailable: contadores ou fuso ilegíveis; nunca responde zeros no lugar"
// @Security		BearerAuth
// @Router			/workflows/{id}/choice-stats [get]
func (h *Handler) Workflow(w http.ResponseWriter, r *http.Request) {
	version, err := versionOf(r)
	if err != nil {
		writeReadError(w, err)
		return
	}
	from, to := period(r)
	report, err := h.reader.Workflow(r.Context(), httpx.WorkflowScope(r), mux.Vars(r)["id"], from, to, version)
	if err != nil {
		writeReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, report)
}

// @Summary		Cliques nos botões de uma campanha oficial
// @Description	Taxa de cliques dos botões do template da campanha (template_button:<índice> para respostas rápidas, template_url:<índice> para botões de link, com tracked indicando se o link é rastreado; um link não rastreado nunca tem cliques). Sem from e to, cobre a vida da campanha (limitada aos 400 dias guardados) e a resposta traz from e to null. Mesmos campos e regras de contagem de GET /workflows/{id}/choice-stats; freeText conta respostas digitadas que citam a mensagem da campanha. O acesso segue a regra de departamento do relatório de disparos; campanha de outro departamento responde campaign_not_found. Exige `whatsapp_campaigns:read`.
// @Tags			WhatsApp Campaigns
// @Produce		json
// @Param			id		path		string	true	"ID da campanha"
// @Param			from	query		string	false	"Primeiro dia (AAAA-MM-DD), junto com to"
// @Param			to		query		string	false	"Último dia (AAAA-MM-DD), junto com from"
// @Success		200		{object}	choicestats.PromptReport
// @Failure		400		{object}	response.ErrorResponse	"invalid_period"
// @Failure		404		{object}	response.ErrorResponse	"campaign_not_found"
// @Failure		503		{object}	response.ErrorResponse	"choice_stats_unavailable"
// @Security		BearerAuth
// @Router			/whatsapp-campaigns/{id}/choice-stats [get]
func (h *Handler) WhatsAppCampaign(w http.ResponseWriter, r *http.Request) {
	access := wc.ScopedReportAccess(middleware.GetWorkspaceID(r), mux.Vars(r)["id"], "", middleware.GetDepartmentFilter(r))
	from, to := period(r)
	report, err := h.reader.WhatsAppCampaign(r.Context(), access, mux.Vars(r)["id"], from, to)
	if err != nil {
		writeReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, report)
}

// @Summary		Cliques no menu de uma campanha não oficial
// @Description	Taxa de cliques de cada opção do menu da campanha (só as opções que o WhatsApp exibe: até 3 botões ou 10 linhas de lista). Sem from e to, cobre a vida da campanha (limitada aos 400 dias guardados) e a resposta traz from e to null. Mesmos campos e regras de contagem de GET /workflows/{id}/choice-stats. Campanha de outro departamento responde campaign_not_found. Exige `unofficial_whatsapp_campaigns:read`.
// @Tags			Unofficial WhatsApp
// @Produce		json
// @Param			id		path		string	true	"ID da campanha"
// @Param			from	query		string	false	"Primeiro dia (AAAA-MM-DD), junto com to"
// @Param			to		query		string	false	"Último dia (AAAA-MM-DD), junto com from"
// @Success		200		{object}	choicestats.PromptReport
// @Failure		400		{object}	response.ErrorResponse	"invalid_period"
// @Failure		404		{object}	response.ErrorResponse	"campaign_not_found"
// @Failure		503		{object}	response.ErrorResponse	"choice_stats_unavailable"
// @Security		BearerAuth
// @Router			/unofficial-whatsapp-campaigns/{id}/choice-stats [get]
func (h *Handler) UnofficialCampaign(w http.ResponseWriter, r *http.Request) {
	scope, allowed := unofficialwahttp.RequestScope(h.departments, r)
	if !allowed {
		response.WriteRefusal(w, refusalCampaignMissing)
		return
	}
	from, to := period(r)
	report, err := h.reader.UnofficialCampaign(r.Context(), middleware.GetWorkspaceID(r), scope, mux.Vars(r)["id"], from, to)
	if err != nil {
		writeReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, report)
}

// @Summary		Cliques nos botões de um template
// @Description	Taxa de cliques de cada botão do template somando todos os envios: fluxos, campanhas oficiais, caixa de entrada e conversas iniciadas. template_button:<índice> são as respostas rápidas e template_url:<índice> os botões de link; tracked é false para um link sem "Rastrear cliques", que nunca tem cliques. As opções seguem os botões atuais do template. Sem from e to, cobre os últimos 400 dias e a resposta traz from e to null. Mesmos campos e regras de contagem de GET /workflows/{id}/choice-stats; freeText conta respostas digitadas que citam a mensagem do template. Exige `whatsapp_templates:read`.
// @Tags			WhatsApp Templates
// @Produce		json
// @Param			id		path		string	true	"ID do template"
// @Param			from	query		string	false	"Primeiro dia (AAAA-MM-DD), junto com to"
// @Param			to		query		string	false	"Último dia (AAAA-MM-DD), junto com from"
// @Success		200		{object}	choicestats.PromptReport
// @Failure		400		{object}	response.ErrorResponse	"invalid_period"
// @Failure		404		{object}	response.ErrorResponse	"template_not_found"
// @Failure		503		{object}	response.ErrorResponse	"choice_stats_unavailable"
// @Security		BearerAuth
// @Router			/whatsapp-templates/{id}/choice-stats [get]
func (h *Handler) Template(w http.ResponseWriter, r *http.Request) {
	from, to := period(r)
	report, err := h.reader.Template(r.Context(), middleware.GetWorkspaceID(r), mux.Vars(r)["id"], from, to)
	if err != nil {
		writeReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, report)
}
