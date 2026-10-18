package lead

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
)

// @Summary		Registrar ou retirar o consentimento do lead
// @Description	Registra uma mudança no consentimento do lead para receber mensagens pelo WhatsApp, com quem registrou (o membro autenticado), a origem, a finalidade e a prova. `kind` é `grant` (registrar) ou `withdraw` (retirar). Por aqui a equipe usa as origens `manual` (registro à mão; um registro exige uma nota em `evidence` com `type` `note` e o texto em `excerpt`), `conversation_request` (o lead pediu numa conversa; `evidence` `message` com o id da mensagem em `ref`) e `api` (integração; qualquer `type` com uma referência em `ref`). As demais origens (`keyword`, `button_reply`, `ai_agent`, `workflow`, `elo`, `form`, `import`) só são gravadas pelo próprio sistema e respondem 403 `lead_consent_source_forbidden`. Retirar exige `leads:update`; registrar exige também `leads:consent` (403 `forbidden`). Retirar marca o lead como "não quer receber mensagens" e apaga o consentimento: a partir daí todo envio para ele é recusado com `lead_opted_out` (campanhas, modelos avulsos, mensagens agendadas e passos de fluxo), menos uma única confirmação de saída configurada pelo usuário; `requestedBy` diz se o lead pediu (`lead_request`) ou se a equipe decidiu (`operator`, o padrão), e volta em `optOutSource`. Registrar desfaz uma retirada anterior. Repetir a mesma mudança não grava nada e responde `changed` false sem `eventId`; uma mudança gravada responde `changed` true e o `eventId` do evento `consent_granted` ou `consent_withdrawn` no histórico do lead (o evento guarda a origem, quem registrou, a finalidade e a referência da prova, nunca o trecho em `excerpt`). Não exige `If-Match`. Recusas 400: `invalid_body`, `lead_consent_kind_invalid`, `lead_consent_source_unknown`, `lead_consent_evidence_required`, `lead_consent_evidence_invalid`, `lead_consent_excerpt_too_long` (mais de 280 caracteres), `lead_consent_purpose_too_long` (mais de 200), `lead_consent_actor_invalid`, `lead_consent_not_allowed`, `lead_opt_out_source_invalid`.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do lead (UUID)"
// @Param			request	body		lead.RecordConsentRequest	true	"A mudança de consentimento"
// @Success		200		{object}	lead.RecordConsentResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/consent [post]
func (h *LeadHandler) RecordConsent(w http.ResponseWriter, r *http.Request) {
	a, ok := h.commandActor(w, r)
	if !ok {
		return
	}
	var req RecordConsentRequest
	if !httpx.DecodeStrictJSON(w, r, &req) {
		return
	}
	out, err := h.commands.RecordConsent(r.Context(), a, mux.Vars(r)["id"], req.toInput())
	if err != nil {
		h.writeCommandError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, RecordConsentResponse{Lead: toLeadRecord(out.Lead, h.now()), Changed: out.Changed, EventID: out.EventID})
}
